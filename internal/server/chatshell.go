package server

// chatshell.go — pre-paint инъекция «чат-шелла» в HTML личного чата (режим 1).
// Плагины dsh включаются после первой отрисовки, поэтому шлюз скрывает
// непрошенные dev-панели и дефолтный бренд до появления первого кадра.
// Брендовые константы приходят из профиля организации (internal/profile):
// ядро agctl организаций не знает.

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/profile"
)

const chatShellMaxHTMLBytes = 1 << 20 // 1 MiB: больше главной страницы dsh на порядок

const chatShellStyle = `<style data-agctl="chat-shell">
svg[viewBox="0 0 23.16 17.04"]{visibility:hidden}
[class*="brandIdentity"],[class*="pXSMma_titleGroup"]{visibility:hidden!important}
[class*="previewBadge"]{display:none!important}
</style>`

// chatShellUserStyle применяется только к обычной роли: правая dev-панель и
// её триггеры скрываются до первого кадра, без JS-collapse после рендера.
const chatShellUserStyle = `<style data-agctl="chat-shell-user">
button[aria-label="Open right sidebar"],button[aria-label="Collapse right sidebar"],
button[aria-label^="Access mode"],button[aria-label^="New terminal"],
button[aria-label^="Workspace files"]{display:none!important}
[class*="_tabLayout_"]:has(button[aria-label="Collapse right sidebar"]){display:none!important}
/* У пользователя нет правого dev-панеля — освобождаем центральную колонку до paint. */
div[style*="grid-template-columns"][class*="_frame"]{grid-template-columns:auto minmax(0px,1fr) minmax(0px,0px)!important}
</style>`

// Document-start: шапка фиксится только в известных UI-селекторах; текст
// сообщения/кода/вставленного пользователем HTML никогда не переписывается.
// Бренд и тэглайн подставляются из профиля (jsq-экранирование json.Marshal).
const chatShellScriptTpl = `<script data-agctl="chat-shell">(function(){
"use strict";
var BRAND=__AGCTL_BRAND_NAME__,TAGLINE=__AGCTL_BRAND_TAGLINE__,LETTER=__AGCTL_BRAND_LETTER__;
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
      var mark=document.createElement('span');mark.dataset.agctlEarlyMark='1';mark.textContent=LETTER;
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
      var title=p.closest&&p.closest('[class*="pXSMma_titleGroup"]');
      var heading=p.closest&&p.closest('[class*="_heading_"]');
      if(title)fixChrome(title);if(heading)fixChrome(heading);
    }
    if(m.addedNodes)for(var j=0;j<m.addedNodes.length;j++){
      var n=m.addedNodes[j];
      if(n.nodeType===1)fixChrome(n);
      else if(n.nodeType===3&&n.parentElement){
        var title=n.parentElement.closest&&n.parentElement.closest('[class*="pXSMma_titleGroup"]');
        var heading=n.parentElement.closest&&n.parentElement.closest('[class*="_heading_"]');
        if(title)fixChrome(title);if(heading)fixChrome(heading);
      }
    }
  }
}).observe(document,{subtree:true,childList:true,characterData:true});
})();</script>`

// jsq — JS-строковый литерал из Go-строки (json.Marshal экранирует и HTML).
func jsq(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

func chatShellScript(v profile.View) string {
	return strings.NewReplacer(
		"__AGCTL_BRAND_NAME__", jsq(v.Name),
		"__AGCTL_BRAND_TAGLINE__", jsq(v.Tagline),
		"__AGCTL_BRAND_LETTER__", jsq(v.Letter),
	).Replace(chatShellScriptTpl)
}

// chatShellHeadForRole — ранняя инъекция. Обычная роль скрывает right dev pane
// до первого кадра; developer/admin получают только бренд и полный нативный dsh UI.
func chatShellHeadForRole(role string, v profile.View) string {
	style := chatShellStyle
	if role != "developer" && role != "admin" {
		style += chatShellUserStyle
	}
	return style + chatShellScript(v)
}

// devShellStyle / devShellScriptTpl — брендинг-only шелл dev-workspace
// (/w/{id}/): заголовок вкладки, строка идентичности (логотип или буква-знак
// вместо кита, имя платформы) и тэглайн. В отличие от чат-шелла панели,
// Workspaces и выборщик моделей не трогаем — это рабочие инструменты режима
// разработки. Плагин chat-brand в dev-домашки не сеется, поэтому ранний скрипт
// шлюза сразу ставит бренд профиля (логотип из профиля; без логотипа — буква).
const devShellStyle = `<style data-agctl="dev-shell">
svg[viewBox="0 0 23.16 17.04"]{visibility:hidden}
</style>`

const devShellScriptTpl = `<script data-agctl="dev-shell">(function(){
"use strict";
var BRAND=__AGCTL_BRAND_NAME__,TAGLINE=__AGCTL_BRAND_TAGLINE__,LOGO=__AGCTL_BRAND_LOGO__,LETTER=__AGCTL_BRAND_LETTER__;
function fixTitle(){if(document.title!==BRAND)document.title=BRAND;}
function makeMark(h){var img=null;if(LOGO){img=document.createElement('img');img.src=LOGO;img.alt='';img.dataset.agctlLogo='1';img.style.height=h+'px';img.style.width='auto';img.style.objectFit='contain';img.style.display='block';return img;}
var s=document.createElement('span');s.dataset.agctlMark='1';s.textContent=LETTER;s.style.cssText='display:inline-flex;align-items:center;justify-content:center;height:'+h+'px;color:#08a8e0;font-size:'+(h-6)+'px;font-weight:800;line-height:1';return s;}
function fixIdentity(root){
  if(!root||!root.querySelectorAll)return;
  var i,k,found;
  var whales=root.querySelectorAll('svg[viewBox="0 0 23.16 17.04"]');
  for(i=0;i<whales.length;i++){
    var w=whales[i];
    var h=Math.max(18,Math.round(w.getBoundingClientRect().height||24));
    w.replaceWith(makeMark(h));
  }
  var rows=[];
  if(root.matches&&root.matches('[class*="brandIdentity"]'))rows.push(root);
  found=root.querySelectorAll('[class*="brandIdentity"]');
  for(i=0;i<found.length;i++)rows.push(found[i]);
  for(i=0;i<rows.length;i++){
    var row=rows[i];
    if(!row.querySelector('[data-agctl-brand-title]')){
      var mark=document.createElement('span');mark.style.cssText='display:inline-flex;align-items:center';
      mark.appendChild(makeMark(28));
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

// devShellHeadFor собирает шелл под профиль: логотип (или буква-знак) из вью.
func devShellHeadFor(v profile.View) string {
	return devShellStyle + strings.NewReplacer(
		"__AGCTL_BRAND_NAME__", jsq(v.Name),
		"__AGCTL_BRAND_TAGLINE__", jsq(v.Tagline),
		"__AGCTL_BRAND_LOGO__", jsq(v.LogoDataURI),
		"__AGCTL_BRAND_LETTER__", jsq(v.Letter),
	).Replace(devShellScriptTpl)
}

// injectShellFor вставляет head-инъекцию в HTML-документ (идемпотентно по
// marker) и подменяет дефолтный <title> dsh на бренд профиля на wire:
// инъекция стоит до <title>, и парсер иначе перезаписал бы document.title
// дефолтом после выполнения раннего скрипта.
func injectShellFor(body []byte, marker, head, title string) []byte {
	if len(body) == 0 {
		return body
	}
	s := string(body)
	if strings.Contains(s, marker) {
		return body
	}
	if title != "" {
		s = strings.Replace(s, "<title>DeepSeek Harness</title>", "<title>"+html.EscapeString(title)+"</title>", 1)
	}
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

// ——— Server-обвязка: бренд живёт в Server, инъекции — его методы. ———

// injectChatShellForRole вставляет чат-шелл под профиль сервера.
func (s *Server) injectChatShellForRole(body []byte, role string) []byte {
	return injectShellFor(body, `<style data-agctl="chat-shell">`, chatShellHeadForRole(role, s.brand), s.brand.Title)
}

// injectDevShell вставляет dev-шелл брендинга в HTML dev-workspace.
func (s *Server) injectDevShell(body []byte) []byte {
	return injectShellFor(body, `<style data-agctl="dev-shell">`, devShellHeadFor(s.brand), s.brand.Title)
}

// injectChatShellResponseForRole — инъекция в http.Response чат-прокси.
func (s *Server) injectChatShellResponseForRole(resp *http.Response, role string) {
	injectShellResponse(resp, func(body []byte) []byte { return s.injectChatShellForRole(body, role) })
}

// injectDevShellResponse — то же для dev-workspace (/w/{id}/).
func (s *Server) injectDevShellResponse(resp *http.Response) {
	injectShellResponse(resp, s.injectDevShell)
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
