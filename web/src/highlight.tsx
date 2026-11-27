import { useMemo } from "react";

// highlight.tsx — подсветка Dockerfile и shell-скриптов без зависимостей:
// небольшой токенайзер (инструкции Dockerfile, ключевые слова shell,
// комментарии, строки в кавычках, переменные ${VAR} и $VAR, флаги --x,
// операторы && || | ; и продолжение строки «\») и <Code> — текст span'ами
// по токенам. Цвета — переменные --hl-* в styles.css (обе темы).

export type Lang = "dockerfile" | "sh" | "text";
type Kind = "" | "kw" | "com" | "str" | "var" | "flag" | "op";
export type Tok = { k: Kind; s: string };

// Инструкции Dockerfile (регистр не важен, принято — заглавными).
const INSTR = new Set([
  "FROM", "RUN", "CMD", "LABEL", "MAINTAINER", "EXPOSE", "ENV", "ADD", "COPY", "ENTRYPOINT",
  "VOLUME", "USER", "WORKDIR", "ARG", "ONBUILD", "STOPSIGNAL", "HEALTHCHECK", "SHELL",
]);
// Инструкции, аргумент которых — команда оболочки.
const SHELL_INSTR = new Set(["RUN", "CMD", "ENTRYPOINT"]);
// Ключевые слова shell — подсвечиваются в позиции команды.
const SH_KW = new Set([
  "if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done", "case", "esac",
  "function", "return", "exit", "local", "export", "readonly", "unset", "shift", "trap",
  "break", "continue", "exec", "eval", "set", "source", "select",
]);
// После этих слов снова позиция команды: if cmd; then cmd; do cmd …
const SH_LEAD = new Set(["if", "then", "else", "elif", "do", "while", "until", "!"]);
// Символы, на которых кончается слово.
const SPECIAL = new Set([" ", "\t", "\r", "\n", "'", '"', "$", "\\", ";", "&", "|", "(", ")", "<", ">", "`"]);

const isSpace = (c: string) => c === " " || c === "\t" || c === "\r";
// Строка в кавычках длиннее — считается незакрытой (подсветка до конца строки).
const MAX_SPAN = 16 << 10;

// tokenize — текст на токены. Эвристика для чтения, а не разбор языка:
// незакрытая кавычка подсвечивается до конца строки.
export function tokenize(text: string, lang: Lang): Tok[] {
  const out: Tok[] = [];
  const push = (k: Kind, s: string) => {
    if (!s) return;
    const last = out[out.length - 1];
    if (last && last.k === k) last.s += s;
    else out.push({ k, s });
  };
  if (lang === "text") {
    push("", text);
    return out;
  }
  const n = text.length;
  let i = 0;
  let lineStart = true; // до первого непробельного символа строки
  let cont = false; // строка кончилась на «\» — следующая продолжает команду
  let cmd = lang === "sh"; // позиция команды: здесь слово может быть ключевым
  let instr = ""; // Dockerfile: инструкция, к которой относится строка
  let expectIn = 0; // for x in … / case "$x" in …: слов до «in»

  const lineEnd = (from: number) => {
    const e = text.indexOf("\n", from);
    return e < 0 ? n : e;
  };
  const atWord = () => i === 0 || /[\s;&|(]/.test(text[i - 1]);
  // varEnd — конец переменной с позиции «$» (from+1 — не переменная).
  const varEnd = (from: number) => {
    const d = text[from + 1] || "";
    if (d === "{") {
      let depth = 0;
      for (let j = from + 1; j < n && text[j] !== "\n"; j++) {
        if (text[j] === "{") depth++;
        else if (text[j] === "}" && --depth === 0) return j + 1;
      }
      return from + 1;
    }
    if (/[A-Za-z_]/.test(d)) {
      let j = from + 2;
      while (j < n && /\w/.test(text[j])) j++;
      return j;
    }
    return /[0-9@#?*!$-]/.test(d) ? from + 2 : from + 1;
  };
  // squoteEnd — позиция после закрывающей «'» (-1 — не закрыта).
  const squoteEnd = (from: number) => {
    const e = text.indexOf("'", from + 1);
    return e < 0 || e - from > MAX_SPAN ? -1 : e + 1;
  };
  // substEnd — позиция после «)» подстановки $( … ) с вложенными кавычками
  // (-1 — не закрыта).
  const substEnd = (from: number): number => {
    let depth = 0;
    const lim = Math.min(n, from + MAX_SPAN);
    for (let j = from + 1; j < lim; j++) {
      const ch = text[j];
      if (ch === "\\") j++;
      else if (ch === "(") depth++;
      else if (ch === ")" && --depth === 0) return j + 1;
      else if (ch === "'" || ch === '"') {
        j = (ch === "'" ? squoteEnd(j) : dquoteEnd(j)) - 1;
        if (j < 0) return -1;
      }
    }
    return -1;
  };
  // dquoteEnd — позиция после закрывающей «"» (-1 — не закрыта); кавычки
  // внутри $( … ) строку не закрывают.
  const dquoteEnd = (from: number): number => {
    const lim = Math.min(n, from + MAX_SPAN);
    for (let j = from + 1; j < lim; j++) {
      if (text[j] === "\\") j++;
      else if (text[j] === "$" && text[j + 1] === "(") {
        j = substEnd(j) - 1;
        if (j < 0) return -1;
      } else if (text[j] === '"') return j + 1;
    }
    return -1;
  };
  // quoteEnd — позиция после закрывающей кавычки q; незакрытая — конец строки.
  const quoteEnd = (from: number, q: string) => {
    const e = q === '"' ? dquoteEnd(from) : squoteEnd(from);
    return e > 0 ? e : lineEnd(from);
  };

  while (i < n) {
    const c = text[i];
    if (c === "\n") {
      push("", c);
      i++;
      lineStart = true;
      if (!cont) {
        cmd = lang === "sh";
        instr = "";
      }
      continue;
    }
    if (isSpace(c)) {
      push("", c);
      i++;
      continue;
    }
    if (c === "#" && (lineStart || atWord())) {
      // комментарий до конца строки; продолжения команды не прерывает
      const e = lineEnd(i);
      push("com", text.slice(i, e));
      i = e;
      lineStart = false;
      continue;
    }
    if (lineStart) {
      lineStart = false;
      const isCont = cont;
      cont = false;
      if (lang === "dockerfile" && !isCont) {
        const m = /^[A-Za-z]+/.exec(text.slice(i, i + 16));
        if (m && INSTR.has(m[0].toUpperCase())) {
          push("kw", m[0]);
          i += m[0].length;
          instr = m[0].toUpperCase();
          cmd = SHELL_INSTR.has(instr);
          continue;
        }
      }
    }
    if (c === "\\") {
      let j = i + 1;
      while (j < n && isSpace(text[j])) j++;
      if (j >= n || text[j] === "\n") {
        // продолжение строки
        push("op", "\\");
        push("", text.slice(i + 1, j));
        i = j;
        cont = true;
        continue;
      }
      push("", text.slice(i, i + 2)); // экранированный символ
      i += 2;
      cmd = false;
      continue;
    }
    if (c === "'") {
      const e = quoteEnd(i, "'");
      push("str", text.slice(i, e));
      i = e;
      cmd = false;
      continue;
    }
    if (c === '"') {
      // строка с переменными внутри
      const e = quoteEnd(i, '"');
      let seg = i;
      for (let j = i + 1; j < e; j++) {
        if (text[j] === "\\") {
          j++;
          continue;
        }
        if (text[j] === "$") {
          const ve = Math.min(varEnd(j), e);
          if (ve > j + 1) {
            push("str", text.slice(seg, j));
            push("var", text.slice(j, ve));
            seg = ve;
            j = ve - 1;
          }
        }
      }
      push("str", text.slice(seg, e));
      i = e;
      cmd = false;
      continue;
    }
    if (c === "$") {
      const e = varEnd(i);
      push(e > i + 1 ? "var" : "", text.slice(i, e));
      i = e;
      cmd = false;
      continue;
    }
    if ((c === "&" && text[i + 1] === "&") || (c === "|" && text[i + 1] === "|")) {
      push("op", text.slice(i, i + 2));
      i += 2;
      cmd = true;
      continue;
    }
    if (c === "|" || c === ";") {
      push("op", c);
      i++;
      cmd = true;
      continue;
    }
    if (SPECIAL.has(c)) {
      push("", c); // ( ) < > & `
      i++;
      cmd = c === "(" || c === "`";
      continue;
    }
    if (c === "-" && atWord() && /[A-Za-z-]/.test(text[i + 1] || "")) {
      let j = i + 1;
      while (j < n && /[\w-]/.test(text[j])) j++;
      push("flag", text.slice(i, j));
      i = j;
      cmd = false;
      continue;
    }
    let j = i;
    while (j < n && !SPECIAL.has(text[j])) j++;
    const w = text.slice(i, j);
    let k: Kind = "";
    let lead = false;
    if (lang === "dockerfile" && instr === "FROM" && /^as$/i.test(w)) k = "kw";
    else if (lang === "dockerfile" && instr === "HEALTHCHECK" && w === "CMD") {
      k = "kw";
      lead = true;
    } else if (cmd && SH_KW.has(w)) {
      k = "kw";
      lead = SH_LEAD.has(w);
    } else if (expectIn > 0 && w === "in") k = "kw";
    else if (cmd && w === "!") lead = true;
    if (k === "kw" && (w === "for" || w === "case" || w === "select")) expectIn = 3;
    else if (k === "kw" && w === "in") expectIn = 0;
    else if (expectIn > 0) expectIn--;
    push(k, w);
    i = j;
    cmd = lead;
  }
  return out;
}

// langOf — язык файла по имени и первой строке: Dockerfile*, .sh и файлы
// с shebang оболочки (#!/bin/sh, #!/usr/bin/env bash).
export function langOf(name: string, text: string): Lang {
  const base = name.split("/").pop() || name;
  if (/^(Dockerfile|Containerfile)/.test(base) || /\.dockerfile$/i.test(base)) return "dockerfile";
  if (/\.(sh|bash|zsh|ksh)$/.test(base)) return "sh";
  const first = text.slice(0, 120).split("\n", 1)[0];
  if (/^#!\s*\S*?\b(sh|bash|dash|zsh|ash|ksh)\b/.test(first)) return "sh";
  return "text";
}

// Больше — без подсветки: столько span'ов браузеру уже тяжело.
const MAX_HIGHLIGHT = 300 << 10;

// Code — текст файла с подсветкой.
export function Code({ text, lang }: { text: string; lang: Lang }) {
  const toks = useMemo(() => (text.length > MAX_HIGHLIGHT ? [{ k: "" as Kind, s: text }] : tokenize(text, lang)), [text, lang]);
  return (
    <pre className="code-view">
      {toks.map((t, i) =>
        t.k ? (
          <span key={i} className={`hl-${t.k}`}>
            {t.s}
          </span>
        ) : (
          t.s
        ),
      )}
    </pre>
  );
}
