#!/usr/bin/env python3
"""make_pptx.py — сборка презентации по фирменным шаблонам университета.

Шаблоны (templates/ рядом со скриптом):
  blue  — Shablon_Siniy.pptx (синий, по умолчанию)
  white — Shablon_bely.pptx (белый)

Шаблон — это готовый макет с тремя слайдами: титульный, контентный
(заголовок/подзаголовок/текст + линия и логотип) и финальный «СПАСИБО…».
Скрипт заполняет титул, делает копии контентного слайда по числу секций
(дизайн сохраняется один в один) и заполняет финал. Никакой ручной возни
с pptx не нужно: описываешь презентацию JSON-планом и запускаешь сборку.

Команды:
  build    --plan plan.json --out deck.pptx [--template blue|white|<path.pptx>]
  inspect  deck.pptx        — дамп слайдов и текстов (проверка результата)
  templates                — список доступных шаблонов

План (JSON):
{
  "template": "blue",
  "title":    "НАЗВАНИЕ ПРЕЗЕНТАЦИИ",
  "subtitle": "подзаголовок (необязательно)",
  "byline":   "2026-11-05 / Иванов И.И.",
  "sections": [
    {"header": "ЗАГОЛОВОК СЛАЙДА", "subheader": "подзаголовок слайда",
     "body": ["первый пункт", "второй пункт"]}
  ],
  "final": "СПАСИБО ЗА ВНИМАНИЕ!"
}

«body» — список пунктов (каждый станет строкой с буллитом) или одна строка
(переносы \\n = абзацы, без буллитов). Отсутствующие необязательные поля
остаются как в шаблоне: subtitle/byline — пустыми на титуле не остаются,
«final» без ключа сохраняет шаблонный текст.
"""

import argparse
import copy
import json
import sys
from pathlib import Path

try:
    from pptx import Presentation
    from pptx.dml.color import RGBColor
    from pptx.util import Inches
except ImportError:  # pragma: no cover
    sys.exit("нужен python-pptx: pip3 install --break-system-packages python-pptx")

HERE = Path(__file__).resolve().parent
TEMPLATES = HERE.parent / "templates"
TEMPLATE_FILES = {
    "blue": "Shablon_Siniy.pptx",
    "white": "Shablon_bely.pptx",
}

# Геометрия контентного слайда: текстовая колонка слева, логотип внизу по
# центру. Тело растягиваем до логотипа, чтобы длинные списки не наезжали.
BODY_HEIGHT_IN = 4.7
BULLET = "•  "


def resolve_template(name: str) -> Path:
    if name in TEMPLATE_FILES:
        return TEMPLATES / TEMPLATE_FILES[name]
    p = Path(name)
    if p.is_file():
        return p
    sys.exit(f"шаблон не найден: {name} (доступны: {', '.join(TEMPLATE_FILES)})")


def text_boxes(slide):
    """Текстовые боксы слайда по вертикали сверху вниз (картинки/линии мимо)."""
    boxes = [sh for sh in slide.shapes if sh.has_text_frame]
    return sorted(boxes, key=lambda sh: (sh.top or 0, sh.left or 0))


def set_text(shape, text: str, bullet: bool = False) -> None:
    """Заменить текст бокса, сохранив формат первого абзаца шаблона.

    Многострочный текст: первая строка — в родной абзац, остальные — копии
    того же абзаца (шрифт/цвет/размер наследуются). Буллит — префиксом.
    """
    tf = shape.text_frame
    lines = text.split("\n") if text else [""]
    # канонический абзац — первый с непустым раном; иначе просто первый
    proto = None
    for p in tf.paragraphs:
        if p.runs:
            proto = p._p
            break
    if proto is None:
        proto = tf.paragraphs[0]._p
    txBody = tf._txBody
    # убрать все абзацы кроме канонического
    paras = txBody.findall(
        "{http://schemas.openxmlformats.org/drawingml/2006/main}p")
    for p in paras:
        if p is not proto:
            txBody.remove(p)
    # в каноническом оставить один ран
    runs = proto.findall(
        "{http://schemas.openxmlformats.org/drawingml/2006/main}r")
    for r in runs[1:]:
        proto.remove(r)
    first = True
    for line in lines:
        if first:
            target = proto
            first = False
        else:
            target = copy.deepcopy(proto)
            txBody.append(target)
        rs = target.findall(
            "{http://schemas.openxmlformats.org/drawingml/2006/main}r")
        text_el = rs[0].find(
            "{http://schemas.openxmlformats.org/drawingml/2006/main}t")
        if text_el is None:
            text_el = rs[0].makeelement(
                "{http://schemas.openxmlformats.org/drawingml/2006/main}t", {})
            rs[0].append(text_el)
        prefix = BULLET if bullet else ""
        text_el.text = f"{prefix}{line}" if line else ""
        # пустые строки без ранов ломаются — держим ран всегда (пустой t ок)
        if not rs:
            pass


def body_text(section) -> str:
    body = section.get("body")
    if body is None:
        return ""
    if isinstance(body, list):
        return "\n".join(str(x) for x in body)
    return str(body)


def duplicate_slide(prs, source):
    """Полная копия слайта (формы + картинки) в конец презентации."""
    layout = source.slide_layout
    dest = prs.slides.add_slide(layout)
    # сносим пустые плейсхолдеры, пришедшие из макета
    for sh in list(dest.shapes):
        sh._element.getparent().remove(sh._element)
    # копии форм
    for sh in source.shapes:
        dest.shapes._spTree.append(copy.deepcopy(sh._element))
    # отношения (картинки): старые rId → новые через временную подмену
    remap = {}
    for rId, rel in list(source.part.rels.items()):
        if rel.is_external or "image" not in rel.reltype:
            continue
        remap[rId] = dest.part.relate_to(rel.target_part, rel.reltype)
    R = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"
    xml = dest.shapes._spTree
    for el in xml.iter():
        for attr in (R + "embed", R + "link"):
            val = el.get(attr)
            if val in remap:
                el.set(attr, f"TMP-{val}")
    for el in xml.iter():
        for attr in (R + "embed", R + "link"):
            val = el.get(attr)
            if val and str(val).startswith("TMP-"):
                el.set(attr, remap.get(val[4:], val[4:]))
    return dest


def reorder_final_to_end(prs) -> None:
    """Последний слайд (финал) держим последним: дубликаты секций
    add_slide кладёт в конец, поэтому финал надо передвинуть."""
    sldIdLst = prs.slides._sldIdLst
    ids = list(sldIdLst)
    final = ids[2]  # после титула и контентного — финальный из шаблона
    sldIdLst.remove(final)
    sldIdLst.append(final)


def build(plan: dict, out: Path, template: str | None) -> None:
    tpl_path = resolve_template(template or plan.get("template") or "blue")
    prs = Presentation(str(tpl_path))
    slides = list(prs.slides)
    if len(slides) < 3:
        sys.exit("ожидал шаблон из 3 слайдов (титул/контент/финал)")
    title_s, content_s, final_s = slides[0], slides[1], slides[2]

    sections = plan.get("sections") or []
    if not sections:
        sys.exit("в плане нет sections — презентация без контента не собирается")

    # титульный слайд: [заголовок, подзаголовок, строка-автор]
    tb = text_boxes(title_s)
    if len(tb) >= 3:
        set_text(tb[0], plan.get("title", ""))
        set_text(tb[1], plan.get("subtitle", ""))
        set_text(tb[2], plan.get("byline", ""))
    else:
        sys.exit(f"титульный слайд: текстовых боксов {len(tb)}, ждём 3")

    # первая секция — прямо в контентный слайд шаблона
    content_slides = [content_s]
    for _ in sections[1:]:
        content_slides.append(duplicate_slide(prs, content_s))
    if len(sections) > 1:
        reorder_final_to_end(prs)

    for slide, sec in zip(content_slides, sections):
        tb = text_boxes(slide)
        if len(tb) < 3:
            sys.exit(f"контентный слайд: текстовых боксов {len(tb)}, ждём 3")
        header, subheader, body = tb[0], tb[1], tb[2]
        set_text(header, sec.get("header", ""))
        set_text(subheader, sec.get("subheader", ""))
        is_list = isinstance(sec.get("body"), list)
        set_text(body, body_text(sec), bullet=is_list)
        try:
            body.height = Inches(BODY_HEIGHT_IN)
        except Exception:
            pass

    # финальный слайд: текст меняется только если ключ задан
    if plan.get("final") is not None:
        tb = text_boxes(final_s)
        if tb:
            set_text(tb[0], str(plan["final"]))

    out.parent.mkdir(parents=True, exist_ok=True)
    prs.save(str(out))
    print(f"OK: {out} ({len(prs.slides.__iter__.__self__._sldIdLst)} слайдов, шаблон {tpl_path.name})")


def inspect(path: Path) -> None:
    prs = Presentation(str(path))
    print(f"{path}: слайдов {len(list(prs.slides))}")
    for i, slide in enumerate(prs.slides):
        print(f"— слайд {i}: макет '{slide.slide_layout.name}'")
        for sh in slide.shapes:
            if sh.has_text_frame and sh.text_frame.text.strip():
                txt = sh.text_frame.text.replace("\n", " / ")
                print(f"    [{sh.shape_type}] {txt[:100]}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    b = sub.add_parser("build", help="собрать презентацию из JSON-плана")
    b.add_argument("--plan", required=True, help="путь к plan.json")
    b.add_argument("--out", required=True, help="файл результата .pptx")
    b.add_argument("--template", default=None, help="blue|white|путь к .pptx")
    i = sub.add_parser("inspect", help="показать слайды и тексты")
    i.add_argument("deck", help="файл .pptx")
    sub.add_parser("templates", help="доступные шаблоны")
    args = ap.parse_args()

    if args.cmd == "build":
        plan = json.loads(Path(args.plan).read_text(encoding="utf-8"))
        build(plan, Path(args.out), args.template)
    elif args.cmd == "inspect":
        inspect(Path(args.deck))
    else:
        for key, name in TEMPLATE_FILES.items():
            p = TEMPLATES / name
            print(f"{key:6} {name} {'OK' if p.is_file() else 'ОТСУТСТВУЕТ'}")


if __name__ == "__main__":
    main()
