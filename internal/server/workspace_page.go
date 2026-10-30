package server

// workspace_page.go — HTML-страница «рабочее место остановлено» вместо голого
// JSON: в «РЕЖИМАХ» клик по остановленному dev-месту раньше отдавал
// {"error":"воркспейс не запущен (статус: stopped)"} и выглядел как сломанный
// dsh. Страница брендируется платформой, объясняет статус честно и умеет
// запустить место (POST /api/workspaces/{id}/start) и вернуть пользователя в
// /w/{id}/, когда статус станет running. API-клиенты по-прежнему получают JSON.

import (
	"html"
	"net/url"
	"strings"

	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/store"
)

const stoppedWorkspaceTpl = `<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>__NAME__ — рабочее место остановлено</title>
<style>
:root{color-scheme:light dark}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;font-family:system-ui,-apple-system,'Segoe UI',sans-serif;background:#f5f7fa;color:#1c2430}
@media (prefers-color-scheme:dark){body{background:#14181f;color:#e8ecf2}}
.card{max-width:470px;margin:1.5rem;padding:2rem 2.25rem;border-radius:14px;background:#fff;box-shadow:0 8px 30px rgba(20,30,50,.08);text-align:center}
@media (prefers-color-scheme:dark){.card{background:#1d232d;box-shadow:0 8px 30px rgba(0,0,0,.35)}}
.brand{display:flex;align-items:center;justify-content:center;gap:8px;font-weight:600;font-size:14px;opacity:.75;margin-bottom:1.25rem}
h1{font-size:1.15rem;margin:.25rem 0 .75rem}
p{margin:.35rem 0;line-height:1.5;opacity:.85}
button{margin-top:1.25rem;padding:.6rem 1.4rem;border:0;border-radius:9px;background:#0e7fd9;color:#fff;font-size:.95rem;cursor:pointer}
button:disabled{opacity:.6;cursor:default}
#agctl-status{margin-top:.8rem;font-size:.85rem;opacity:.7;min-height:1.2em}
</style></head><body><div class="card">
<div class="brand">__BRAND_MARK__ __BRAND_NAME__</div>
<h1>Рабочее место «__NAME__» сейчас остановлено</h1>
<p>Интерфейс DSH для этого места не запущен: контейнер остановлен. Это нормально — репозиторий, сессии и файлы остаются на диске и переживают остановку.</p>
<button id="agctl-start">Запустить рабочее место</button>
<div id="agctl-status"></div>
</div>
<script>(function(){
var btn=document.getElementById('agctl-start'),st=document.getElementById('agctl-status');
function poll(){
  fetch('/api/workspaces/__ID__', {credentials:'same-origin'}).then(function(r){return r.ok?r.json():null}).then(function(j){
    var s=j&&j.workspace&&j.workspace.status;
    if(s==='running'){location.replace('/w/__ID__/');return;}
    st.textContent=s==='provisioning'?'Место создаётся…':'Статус: '+(s||'неизвестен')+' — ждём запуска…';
    setTimeout(poll,2000);
  }).catch(function(){st.textContent='Связь с сервером пропала — пробуем ещё…';setTimeout(poll,2500);});
}
btn.addEventListener('click',function(){
  btn.disabled=true;st.textContent='Запускаем рабочее место…';
  fetch('/api/workspaces/__ID__/start',{method:'POST',credentials:'same-origin'}).catch(function(){});
  poll();
});
})();</script>
</body></html>`

// stoppedWorkspacePage брендирует страницу профилем организации: логотип
// (или буква-знак без логотипа) и имя платформы.
func stoppedWorkspacePage(ws store.Workspace, v profile.View) string {
	mark := "<span style=\"display:inline-flex;align-items:center;justify-content:center;height:22px;color:#08a8e0;font-size:16px;font-weight:800;line-height:1\">" + html.EscapeString(v.Letter) + "</span>"
	if v.LogoDataURI != "" {
		mark = "<img src=\"" + v.LogoDataURI + "\" alt=\"\" style=\"height:22px;width:auto\">"
	}
	return strings.NewReplacer(
		"__NAME__", html.EscapeString(ws.Name),
		"__ID__", url.PathEscape(ws.ID),
		"__BRAND_MARK__", mark,
		"__BRAND_NAME__", html.EscapeString(v.Name),
	).Replace(stoppedWorkspaceTpl)
}
