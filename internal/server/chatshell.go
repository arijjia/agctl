package server

// chatshell.go — pre-paint инъекция «чат-шелла» в HTML личного чата (режим 1).
// Плагины dsh включаются после первой отрисовки, поэтому шлюз скрывает
// непрошенные dev-панели и дефолтный бренд до появления первого кадра.

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/assets"
)

const chatShellMaxHTMLBytes = 1 << 20 // 1 MiB: больше главной страницы dsh на порядок

const chatShellStyle = `<style data-agctl="chat-shell">
button[aria-label="Open right sidebar"],button[aria-label="Collapse right sidebar"],
button[aria-label^="Access mode"],button[aria-label^="New terminal"],
button[aria-label^="Workspace files"],button[aria-label="Choose workspace"],
button[aria-label="Add workspace"],button[aria-label="View options"],
button[aria-label^="Workspace actions for "],button[aria-label^="New session in "]{display:none!important}
[class*="projectRow"]{display:none!important}
[class*="_tabLayout_"]:has(button[aria-label="Collapse right sidebar"]){display:none!important}
/* Грид фрейма dsh резервирует третью колонку под (скрытую) правую панель —
   сохранённое состояние ширины не должно съедать половину чата. */
div[style*="grid-template-columns"][class*="_frame"]{grid-template-columns:auto minmax(0px,1fr) minmax(0px,0px)!important}
svg[viewBox="0 0 23.16 17.04"]{visibility:hidden}
[class*="brandIdentity"],[class*="pXSMma_titleGroup"]{visibility:hidden!important}
[class*="previewBadge"]{display:none!important}
</style>`

// Document-start: шапка фиксится только в известных UI-селекторах; текст
// сообщения/кода/вставленного пользователем HTML никогда не переписывается.
const chatShellScript = `<script data-agctl="chat-shell">(function(){
"use strict";
var BRAND="ИИ-мастерская ТюмГУ",TAGLINE="Тюмень на правильном пути";
function fixTitle(){if(document.title!==BRAND)document.title=BRAND;}
function setLeafText(root,pattern,value){
  if(!root||!root.querySelectorAll)return false;
  var found=false,all=root.querySelectorAll('*');
  for(var i=0;i<all.length;i++){
    var el=all[i];
    if(el.children.length!==0)continue;
    var text=el.textContent||'';
    if(pattern.test(text)){el.textContent=value;found=true;}
    else if(text===value)found=true;
  }
  return found;
}
function fixIdentity(root){
  if(!root||!root.querySelectorAll)return;
  var rows=[];
  if(root.matches&&root.matches('[class*="brandIdentity"]'))rows.push(root);
  var found=root.querySelectorAll('[class*="brandIdentity"]');
  for(var i=0;i<found.length;i++)rows.push(found[i]);
  for(var i=0;i<rows.length;i++){
    var row=rows[i];
    if(!row.querySelector('[data-agctl-early-brand],[data-agctl-brand-title]')){
      var mark=document.createElement('span');mark.dataset.agctlEarlyMark='1';mark.textContent='Т';
      mark.style.cssText='display:inline-flex;align-items:center;justify-content:center;width:28px;height:28px;color:#08a8e0;font-size:22px;font-weight:800;line-height:1';
      var title=document.createElement('span');title.dataset.agctlEarlyBrand='1';title.textContent=BRAND;
      title.style.cssText='font-size:14px;font-weight:600;line-height:1.1;white-space:nowrap';
      row.replaceChildren(mark,title);row.style.display='inline-flex';row.style.alignItems='center';row.style.gap='8px';row.style.overflow='visible';
    }
    row.style.setProperty('visibility','visible','important');
  }
}
function fixChrome(root){
  if(!root||!root.querySelectorAll)return;
  fixIdentity(root);
  var labels=[];
  if(root.matches&&root.matches('[class*="sectionLabel"]'))labels.push(root);
  var found=root.querySelectorAll('[class*="sectionLabel"]');
  for(var i=0;i<found.length;i++)labels.push(found[i]);
  for(var i=0;i<labels.length;i++)if((labels[i].textContent||'').trim()==='Workspaces')labels[i].textContent='Сессии';
  var titles=[];
  if(root.matches&&root.matches('[class*="pXSMma_titleGroup"]'))titles.push(root);
  found=root.querySelectorAll('[class*="pXSMma_titleGroup"]');
  for(var i=0;i<found.length;i++)titles.push(found[i]);
  for(var i=0;i<titles.length;i++){
    if(setLeafText(titles[i],/Into the Unknown/i,TAGLINE))titles[i].style.setProperty('visibility','visible','important');
  }
  var heads=[];
  if(root.matches&&root.matches('[class*="_heading_"]'))heads.push(root);
  found=root.querySelectorAll('[class*="_heading_"]');
  for(var i=0;i<found.length;i++)heads.push(found[i]);
  for(var i=0;i<heads.length;i++){
    var h=heads[i];
    if((h.textContent||'').trim()!=='DeepSeek')continue;
    var group=h.parentElement;
    if(group&&group.parentElement&&group.parentElement.matches('[class*="_7KE1Ra_groups"]'))group.style.setProperty('display','none','important');
    else h.style.setProperty('display','none','important');
  }
}
fixTitle();
fixChrome(document);
new MutationObserver(function(muts){
  fixTitle();
  for(var i=0;i<muts.length;i++){
    var m=muts[i];
    if(m.type==='characterData'&&m.target.parentElement){
      var p=m.target.parentElement;
      var label=p.closest&&p.closest('[class*="sectionLabel"]');
      var title=p.closest&&p.closest('[class*="pXSMma_titleGroup"]');
      var heading=p.closest&&p.closest('[class*="_heading_"]');
      if(label)fixChrome(label);if(title)fixChrome(title);if(heading)fixChrome(heading);
    }
    if(m.addedNodes)for(var j=0;j<m.addedNodes.length;j++){
      var n=m.addedNodes[j];
      if(n.nodeType===1)fixChrome(n);
      else if(n.nodeType===3&&n.parentElement){
        var label=n.parentElement.closest&&n.parentElement.closest('[class*="sectionLabel"]');
        var title=n.parentElement.closest&&n.parentElement.closest('[class*="pXSMma_titleGroup"]');
        var heading=n.parentElement.closest&&n.parentElement.closest('[class*="_heading_"]');
        if(label)fixChrome(label);if(title)fixChrome(title);if(heading)fixChrome(heading);
      }
    }
  }
}).observe(document,{subtree:true,childList:true,characterData:true});
})();</script>`

// chatShellHead — инъекция, добавляемая сразу после открывающего <head>.
var chatShellHead = chatShellStyle + chatShellScript

// devShellStyle / devShellScriptTpl — брендинг-only шелл dev-workspace
// (/w/{id}/): заголовок вкладки, строка идентичности (логотип вместо кита,
// «ИИ-мастерская ТюмГУ») и тэглайн «Тюмень на правильном пути». В отличие от
// чат-шелла панели, Workspaces и выборщик моделей не трогаем — это рабочие
// инструменты режима разработки. Плагин chat-brand в dev-домашки не сеется,
// поэтому ранний скрипт шлюза сразу ставит настоящий логотип (тот же PNG, что
// в chat-brand: internal/assets/brand/logo.png).
const devShellStyle = `<style data-agctl="dev-shell">
svg[viewBox="0 0 23.16 17.04"]{visibility:hidden}
</style>`

const devShellScriptTpl = `<script data-agctl="dev-shell">(function(){
"use strict";
var BRAND="ИИ-мастерская ТюмГУ",TAGLINE="Тюмень на правильном пути",LOGO="data:image/png;base64,__AGCTL_LOGO__";
function fixTitle(){if(document.title!==BRAND)document.title=BRAND;}
function makeLogo(h){var img=document.createElement('img');img.src=LOGO;img.alt='';img.dataset.agctlLogo='1';img.style.height=h+'px';img.style.width='auto';img.style.objectFit='contain';img.style.display='block';return img;}
function fixIdentity(root){
  if(!root||!root.querySelectorAll)return;
  var i,k,found;
  var whales=root.querySelectorAll('svg[viewBox="0 0 23.16 17.04"]');
  for(i=0;i<whales.length;i++){
    var w=whales[i];
    var h=Math.max(18,Math.round(w.getBoundingClientRect().height||24));
    w.replaceWith(makeLogo(h));
  }
  var rows=[];
  if(root.matches&&root.matches('[class*="brandIdentity"]'))rows.push(root);
  found=root.querySelectorAll('[class*="brandIdentity"]');
  for(i=0;i<found.length;i++)rows.push(found[i]);
  for(i=0;i<rows.length;i++){
    var row=rows[i];
    if(!row.querySelector('[data-agctl-brand-title]')){
      var mark=document.createElement('span');mark.style.cssText='display:inline-flex;align-items:center';
      mark.appendChild(makeLogo(28));
      var title=document.createElement('span');title.dataset.agctlBrandTitle='1';title.textContent=BRAND;
      title.style.cssText='font-size:14px;font-weight:600;line-height:1.1;white-space:nowrap';
      row.replaceChildren(mark,title);row.style.display='inline-flex';row.style.alignItems='center';row.style.gap='8px';row.style.overflow='visible';
    }
    row.style.setProperty('visibility','visible','important');
  }
  var tags=[];
  if(root.matches&&root.matches('[class*="pXSMma_titleGroup"]'))tags.push(root);
  found=root.querySelectorAll('[class*="pXSMma_titleGroup"]');
  for(i=0;i<found.length;i++)tags.push(found[i]);
  for(i=0;i<tags.length;i++){
    var g=tags[i];
    var leaves=g.querySelectorAll('*');
    for(k=0;k<leaves.length;k++){
      var el=leaves[k];
      if(el.children.length)continue;
      var t=el.textContent||'';
      if(/Into the Unknown/i.test(t)){el.textContent=t.replace(/Into the Unknown/gi,TAGLINE);g.style.setProperty('visibility','visible','important');}
      else if(t===TAGLINE){g.style.setProperty('visibility','visible','important');}
    }
  }
}
fixTitle();fixIdentity(document);
new MutationObserver(function(muts){
  fixTitle();
  for(var i=0;i<muts.length;i++){
    var m=muts[i];
    if(m.addedNodes)for(var j=0;j<m.addedNodes.length;j++){
      var n=m.addedNodes[j];
      if(n.nodeType===1)fixIdentity(n);
    }
    if(m.type==='characterData'&&m.target.parentElement&&m.target.parentElement.closest){
      var g=m.target.parentElement.closest('[class*="pXSMma_titleGroup"]');
      if(g)fixIdentity(g);
    }
  }
}).observe(document,{subtree:true,childList:true,characterData:true});
})();</script>`

// devShellHead собирается один раз: логотип вшивается base64 из ассетов.
var devShellHead = devShellStyle + strings.ReplaceAll(
	devShellScriptTpl, "__AGCTL_LOGO__",
	base64.StdEncoding.EncodeToString(assets.BrandLogo))

// injectChatShell вставляет чат-шелл в HTML-документ (идемпотентно).
func injectChatShell(body []byte) []byte {
	return injectShell(body, `<style data-agctl="chat-shell">`, chatShellHead)
}

// injectDevShell вставляет dev-шелл брендинга в HTML dev-workspace.
func injectDevShell(body []byte) []byte {
	return injectShell(body, `<style data-agctl="dev-shell">`, devShellHead)
}

// injectShell вставляет head-инъекцию в HTML-документ (идемпотентно по marker).
func injectShell(body []byte, marker, head string) []byte {
	if len(body) == 0 {
		return body
	}
	s := string(body)
	if strings.Contains(s, marker) {
		return body
	}
	// Заголовок меняем на wire тоже: инъекция стоит до <title>, и парсер иначе
	// перезаписал бы document.title дефолтом после выполнения раннего скрипта.
	s = strings.Replace(s, "<title>DeepSeek Harness</title>", "<title>ИИ-мастерская ТюмГУ</title>", 1)
	if i := openingTagEnd(s, "head"); i >= 0 {
		return []byte(s[:i] + head + s[i:])
	}
	if i := openingTagEnd(s, "body"); i >= 0 {
		return []byte(s[:i] + head + s[i:])
	}
	if i := doctypeEnd(s); i >= 0 {
		return []byte(s[:i] + head + s[i:])
	}
	return []byte(head + s)
}

// openingTagEnd — HTML-aware scan до открывающего тега name. Комментарии,
// quoted attributes и raw-text элементы пропускаются, поэтому literal
// "</head>" внутри <script> не может разрезать bootstrap-документ.
func openingTagEnd(s, name string) int {
	lower := asciiLower(s)
	for pos := 0; pos < len(s); {
		rel := strings.IndexByte(s[pos:], '<')
		if rel < 0 {
			return -1
		}
		i := pos + rel
		if strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return -1
			}
			pos = i + 4 + end + 3
			continue
		}
		if i+1 >= len(s) || s[i+1] == '!' || s[i+1] == '?' {
			end := htmlTagEnd(s, i+1)
			if end < 0 {
				return -1
			}
			pos = end + 1
			continue
		}
		j := i + 1
		closing := false
		if s[j] == '/' {
			closing = true
			j++
		}
		start := j
		for j < len(s) && isHTMLNameByte(s[j]) {
			j++
		}
		if j == start {
			pos = i + 1
			continue
		}
		tag := lower[start:j]
		end := htmlTagEnd(s, j)
		if end < 0 {
			return -1
		}
		if !closing && tag == name {
			return end + 1
		}
		pos = end + 1
		if !closing && isHTMLRawText(tag) {
			close := rawTextClose(lower, pos, tag)
			if close < 0 {
				return -1
			}
			closeEnd := htmlTagEnd(s, close+2+len(tag))
			if closeEnd < 0 {
				return -1
			}
			pos = closeEnd + 1
		}
	}
	return -1
}

func htmlTagEnd(s string, start int) int {
	var quote byte
	for i := start; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		} else if c == '>' {
			return i
		}
	}
	return -1
}

func rawTextClose(lower string, from int, name string) int {
	needle := "</" + name
	for from < len(lower) {
		rel := strings.Index(lower[from:], needle)
		if rel < 0 {
			return -1
		}
		i := from + rel
		end := i + len(needle)
		if end == len(lower) || lower[end] == '>' || lower[end] == '/' || lower[end] == ' ' || lower[end] == '\n' || lower[end] == '\t' || lower[end] == '\r' {
			return i
		}
		from = end
	}
	return -1
}

func isHTMLNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ':' || c == '-'
}

// asciiLower меняет только ASCII-теги; byte offsets остаются точными даже
// если до тега встречается Unicode-текст с многобайтовым case mapping.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func isHTMLRawText(tag string) bool {
	switch tag {
	case "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes":
		return true
	default:
		return false
	}
}

func doctypeEnd(s string) int {
	lower := asciiLower(s)
	i := strings.Index(lower, "<!doctype")
	if i < 0 {
		return -1
	}
	if end := htmlTagEnd(s, i+2); end >= 0 {
		return end + 1
	}
	return -1
}

// injectChatShellResponse ограничивает память и никогда не теряет прочитанный
// префикс: при ошибке чтения/слишком большом HTML прокси отдает оригинальный
// поток без модификации.
func injectChatShellResponse(resp *http.Response) {
	injectShellResponse(resp, injectChatShell)
}

// injectDevShellResponse — то же для dev-workspace (/w/{id}/).
func injectDevShellResponse(resp *http.Response) {
	injectShellResponse(resp, injectDevShell)
}

func injectShellResponse(resp *http.Response, inject func([]byte) []byte) {
	if resp == nil || resp.Body == nil || resp.StatusCode != http.StatusOK ||
		!strings.Contains(resp.Header.Get("Content-Type"), "text/html") ||
		resp.Header.Get("Content-Encoding") != "" {
		return
	}
	original := resp.Body
	body, err := io.ReadAll(io.LimitReader(original, chatShellMaxHTMLBytes+1))
	if err != nil || len(body) > chatShellMaxHTMLBytes {
		resp.Body = &restoredBody{Reader: io.MultiReader(bytes.NewReader(body), original), closer: original}
		return
	}
	_ = original.Close()
	injected := inject(body)
	if len(injected) == len(body) && bytes.Equal(injected, body) {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return
	}
	resp.Body = io.NopCloser(bytes.NewReader(injected))
	resp.ContentLength = int64(len(injected))
	resp.TransferEncoding = nil
	resp.Header.Del("Transfer-Encoding")
	resp.Header.Set("Content-Length", strconv.Itoa(len(injected)))
}

type restoredBody struct {
	io.Reader
	closer io.Closer
}

func (b *restoredBody) Close() error { return b.closer.Close() }
