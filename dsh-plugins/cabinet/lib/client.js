// agctl cabinet v3.2 — профиль пользователя: чип в нативном слоте
// sidebar.footer.action, popup (Профиль / Справка); родная Settings-кнопка
// остаётся отдельным рядом под ним. Профиль показывает роль, статистику,
// заявки и SSH. Runtime'ом управляет только agctl serve — кнопок запуска/останова нет.
window.__ModuleLoader__.load({
	id: "@arijjia/agctl-cabinet",
	factory: (require) => {
		var exports = {};
		const React = require("react");

		function ensureStyles() {
			if (document.querySelector("style[data-agctl-cabinet-styles]")) return;
			const style = document.createElement("style");
			style.dataset.agctlCabinetStyles = "1";
			style.textContent = `
				[data-agctl-cabinet-chip]:hover { background: var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,.08)) !important; }
				[data-agctl-cabinet-popup] button:hover { background: var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,.08)) !important; }
				[data-agctl-cabinet-overlay] input, [data-agctl-cabinet-overlay] textarea {
					box-sizing:border-box;width:100%;min-height:38px;padding:8px 11px;margin-top:4px;
					border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:8px;
					background:transparent;color:inherit;font:inherit;resize:vertical;
				}
				[data-agctl-cabinet-overlay] .agctl-btn {
					padding:8px 14px;border:1px solid #1459b8;border-radius:8px;background:#1769d2;
					color:#fff;cursor:pointer;font:inherit;font-weight:650;
				}
				[data-agctl-cabinet-overlay] .agctl-btn:disabled { opacity:.6; cursor:not-allowed; }
				[data-agctl-cabinet-overlay] .agctl-card {
					border:1px solid var(--dsw-alias-border-l1,#333);border-radius:12px;
					padding:14px 16px;margin:0 0 14px;
				}
			`;
			document.head.appendChild(style);
		}

		function initials(login) {
			const clean = String(login || "").replace(/[^\p{L}\p{N}]+/gu, " ").trim();
			const parts = clean.split(/\s+/).filter(Boolean);
			if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
			return clean.slice(0, 2).toUpperCase() || "··";
		}

		function roleName(role) {
			return { admin: "Администратор", developer: "Разработчик", user: "Пользователь" }[role] || role;
		}

		function roleDescription(role) {
			switch (role) {
				case "admin": return "Полный доступ: пользователи, заявки на роль, аудит, SSH и все developer-возможности.";
				case "developer": return "Всё пользователя плюс SSH-канал к своему рантайму, клонирование проектов из GitHub и developer-настройки.";
				default: return "Личный чат и воркспейсы-проекты. SSH и developer-настройки — после заявки на роль developer.";
			}
		}

		function el(tag, css, text) {
			const node = document.createElement(tag);
			if (css) node.style.cssText = css;
			if (text !== undefined) node.textContent = text;
			return node;
		}

		function apiFactory() {
			return async (path, method = "GET", body) => {
				const res = await fetch(path, {
					credentials: "include", method,
					headers: { Accept: "application/json", ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
					body: body === undefined ? undefined : JSON.stringify(body),
				});
				let data;
				try { data = await res.json(); } catch { data = null; }
				if (!res.ok) throw new Error("HTTP " + res.status + ": " + ((data && data.error) || "Запрос не выполнен"));
				return data;
			};
		}

		// ── статистика активности из родного стора сессий dsh ───────────────────
		function sessionActivity() {
			const root = window.__agctlCabinetCtx;
			const sessions = root && root.get && root.get("sessions");
			try {
				const snap = sessions && sessions.list && sessions.list.getSnapshot();
				if (!snap || !Array.isArray(snap.ids)) return { total: 0, byDay: new Map(), usage: null, usageSessions: 0 };
				const byDay = new Map();
				const usage = { uncachedInputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 };
				let total = 0, usageSessions = 0;
				for (const id of snap.ids) {
					const s = snap.byId[id];
					if (!s || s.origin === "subagent") continue;
					total++;
					const day = Math.floor((s.updatedAt || 0) / 86400000);
					if (day > 0) byDay.set(day, (byDay.get(day) || 0) + 1);
					// dsh token-meter exposes cumulative provider-reported totals per
					// session in the native list projection; no guesses from message text.
					const projection = s.projectionValues || (snap.projectionsBySession && snap.projectionsBySession[id] && snap.projectionsBySession[id].values);
					const tokens = projection && projection.tokenUsage;
					if (tokens) {
						usageSessions++;
						for (const key of Object.keys(usage)) usage[key] += Number(tokens[key]) || 0;
					}
				}
				return { total, byDay, usage, usageSessions };
			} catch (e) {
				return { total: 0, byDay: new Map(), usage: null, usageSessions: 0 };
			}
		}

		function heatColor(count) {
			if (!count) return null;
			if (count <= 1) return "rgba(35,110,220,.35)";
			if (count <= 3) return "rgba(35,110,220,.6)";
			if (count <= 6) return "rgba(35,110,220,.8)";
			return "rgba(35,110,220,1)";
		}

		function buildOverlay() {
			closeOverlay();
			const overlay = el("div", "position:fixed;inset:0;z-index:10001;background:rgba(0,0,0,.55);display:flex;align-items:flex-start;justify-content:center;padding:48px 20px;overflow:auto");
			overlay.dataset.agctlCabinetOverlay = "1";
			const sheet = el("div", "width:min(760px,100%);background:var(--dsw-alias-bg-layer-1,#161616);color:var(--dsw-alias-label-primary,inherit);border-radius:16px;border:1px solid var(--dsw-alias-border-l1,#333);padding:28px 30px;box-shadow:0 24px 80px rgba(0,0,0,.5);box-sizing:border-box");
			const close = el("button", "float:right;border:none;background:transparent;color:inherit;font-size:22px;cursor:pointer;line-height:1", "×");
			close.setAttribute("aria-label", "Закрыть");
			close.addEventListener("click", () => overlay.remove());
			sheet.appendChild(close);
			overlay.appendChild(sheet);
			overlay.addEventListener("click", (ev) => { if (ev.target === overlay) overlay.remove(); });
			document.body.appendChild(overlay);
			return sheet;
		}

		function openHelpOverlay() {
			const sheet = buildOverlay();
			const box = el("div", null, null);
			box.innerHTML = `
				<h2 style="margin:0 0 14px;font-size:18px">Справка — ИИ-мастерская ТюмГУ</h2>
				<ul style="margin:0;padding-left:18px;display:flex;flex-direction:column;gap:10px;font-size:13px;line-height:1.5">
					<li><b>Воркспейсы</b> — папки проектов в боковой панели. Новая сессия, созданная внутри папки, работает в каталоге этого проекта.</li>
					<li><b>Создать воркспейс</b> — кнопка в шапке списка Workspaces: пустой или клон публичного GitHub-репозитория.</li>
					<li><b>SSH</b> (роль developer) — положи публичный ключ в Профиле; порт появится, когда рантайм работает. Доступ только с самого сервера или через туннель.</li>
					<li><b>Рантайм</b> управляется agctl автоматически: просыпается при открытии чата и засыпает после часа бездействия; активная SSH-сессия продлевает работу. Запускать и останавливать вручную не нужно.</li>
					<li><b>Git</b> — история воркспейса сохраняется автоматически; автопуш во внешний Git не выполняется.</li>
				</ul>`;
			sheet.appendChild(box);
		}

		function statsBlock() {
			const wrap = el("div", null, null);
			const { total, byDay, usage, usageSessions } = sessionActivity();
			const fmt = (n) => new Intl.NumberFormat("ru-RU").format(Math.round(n || 0));
			const input = usage ? usage.uncachedInputTokens + usage.cacheReadTokens + usage.cacheWriteTokens : 0;
			const output = usage ? usage.outputTokens : 0;
			const tokenTotal = input + output;
			const row = el("div", "display:flex;gap:10px;flex-wrap:wrap;margin:12px 0 6px");
			const card = (value, label) => {
				const c = el("div", "flex:1 1 145px;border:1px solid var(--dsw-alias-border-l1,#333);border-radius:12px;padding:12px 14px;text-align:center");
				c.appendChild(el("div", "font-size:20px;font-weight:700", value));
				c.appendChild(el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);margin-top:4px", label));
				return c;
			};
			row.append(
				card(usageSessions ? fmt(tokenTotal) : "—", "Токенов всего"),
				card(usageSessions ? fmt(input) : "—", "Входные токены"),
				card(usageSessions ? fmt(output) : "—", "Выходные токены"),
				card(fmt(total), "Сессий всего"),
				card(fmt(byDay.size), "Активных дней"),
			);
			wrap.appendChild(row);
			const cache = usage && usageSessions ? el("p", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);margin:8px 0 0",
				`Кэш: чтение ${fmt(usage.cacheReadTokens)}, запись ${fmt(usage.cacheWriteTokens)}; учтено сессий: ${usageSessions}`)
				: el("p", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);margin:8px 0 0", "Token usage пока не пришёл от dsh.");
			wrap.appendChild(cache);
			const label = el("div", "font-size:12px;font-weight:600;color:var(--dsw-alias-label-secondary,#999);margin-top:14px", "Активность сессий за год (по последнему обновлению; не токены/день)");
			wrap.appendChild(label);
			const heatScroll = el("div", "overflow-x:auto;margin-top:8px");
			const heat = el("div", "display:grid;grid-template-rows:repeat(7, 10px);grid-auto-flow:column;grid-auto-columns:10px;gap:3px;width:max-content");
			const today = Math.floor(Date.now() / 86400000);
			for (let i = 363; i >= 0; i--) {
				const day = today - i;
				const count = byDay.get(day) || 0;
				const cell = el("div", "width:10px;height:10px;border-radius:3px", null);
				cell.style.background = heatColor(count) || "var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,.08))";
				cell.title = count ? `${count} сессий` : "Нет активности";
				heat.appendChild(cell);
			}
			heatScroll.appendChild(heat);
			wrap.appendChild(heatScroll);
			return wrap;
		}

		function openProfileOverlay(me) {
			const sheet = buildOverlay();
			const api = apiFactory();
			const status = el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);white-space:pre-wrap", null);

			// шапка
			const head = el("div", "display:flex;flex-direction:column;align-items:center;gap:8px;margin:6px 0 22px");
			const displayName = me.displayName || me.name || me.login;
			const avatar = el("div", "width:84px;height:84px;border-radius:50%;display:flex;align-items:center;justify-content:center;background:#8a9a5b;color:#fff;font-size:30px;font-weight:700", initials(displayName));
			const name = el("div", "font-size:22px;font-weight:700", displayName);
			const handle = el("div", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "@" + me.login);
			head.append(avatar, name, handle);
			sheet.append(head);

			// роль
			const roleCard = el("div", "agctl-card");
			roleCard.appendChild(el("h2", "margin:0 0 10px;font-size:14px;font-weight:600", "Права доступа"));
			const roleRow = el("div", "display:flex;gap:10px;align-items:baseline");
			roleRow.appendChild(el("span", "font-size:12px;font-weight:600;color:var(--dsw-alias-label-secondary,#999);width:130px;flex:none", "Роль"));
			const badge = el("span", "display:inline-block;padding:2px 10px;border-radius:999px;border:1px solid #1459b8;background:rgba(23,105,210,.15);font-size:12px;font-weight:700", roleName(me.role));
			roleRow.appendChild(badge);
			roleCard.append(roleRow, el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);margin:8px 0 0", roleDescription(me.role)));
			sheet.appendChild(roleCard);

			// использование
			const usageCard = el("div", "agctl-card");
			usageCard.appendChild(el("h2", "margin:0 0 4px;font-size:14px;font-weight:600", "Использование"));
			usageCard.appendChild(statsBlock());
			const runtimeLine = el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);margin:10px 0 0", "");
			usageCard.appendChild(runtimeLine);
			sheet.appendChild(usageCard);

			const isDev = me.role === "developer" || me.role === "admin";

			// SSH (developer/admin)
			if (isDev) {
				const sshCard = el("div", "agctl-card");
				sshCard.appendChild(el("h2", "margin:0 0 10px;font-size:14px;font-weight:600", "SSH"));
				const label = el("label", "display:flex;flex-direction:column;gap:4px;font-size:12px;font-weight:600", "Публичный SSH-ключ (пусто — отключить SSH)");
				const area = el("textarea", null, null);
				area.placeholder = "ssh-ed25519 …";
				label.appendChild(area);
				const save = el("button", "agctl-btn", "Сохранить ключ");
				const portLine = el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "");
				const note = el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "SSH слушает только loopback 127.0.0.1 сервера. Для удалённого подключения нужен SSH-туннель. Приватный ключ не отправляй.");
				save.addEventListener("click", async () => {
					save.disabled = true;
					try {
						const data = await api("/api/me/ssh", "PUT", { pubKey: area.value.trim() });
						area.value = data.pubKey || "";
						status.textContent = data.note || "SSH-ключ сохранён.";
					} catch (err) { status.textContent = String(err.message || err); }
					finally { save.disabled = false; }
				});
				sshCard.append(label, save, portLine, note);
				sheet.appendChild(sshCard);
				api("/api/me/ssh").then((ssh) => {
					area.value = ssh.pubKey || "";
					portLine.textContent = ssh.sshPort > 0
						? "ssh root@127.0.0.1 -p " + ssh.sshPort
						: "Рантайм спит (sshPort = 0): открой чат — sshd поднимется при следующем старте.";
				}).catch((err) => { status.textContent = "SSH: " + err.message; });
			}

			// заявки на роль
			const requestsCard = el("div", null, null);
			const requestsTitle = el("h2", "margin:0 0 10px;font-size:14px;font-weight:600", me.role === "admin" ? "Заявки на роль developer" : "Мои заявки");
			requestsCard.appendChild(requestsTitle);
			sheet.append(requestsCard, status);

			api("/api/me/runtime").then((runtime) => {
				runtimeLine.textContent = runtime.status === "running"
					? "Личный рантайм работает; заснёт сам после часа простоя."
					: "Личный рантайм спит; проснётся сам при открытии чата.";
			}).catch(() => { runtimeLine.textContent = ""; });

			if (me.role === "user") {
				const form = el("div", "agctl-card");
				form.appendChild(el("h2", "margin:0 0 10px;font-size:14px;font-weight:600", "Заявка на роль developer"));
				const label = el("label", "display:flex;flex-direction:column;gap:4px;font-size:12px;font-weight:600", "Мотивация");
				const area = el("textarea", "min-height:90px", null);
				label.appendChild(area);
				const send = el("button", "agctl-btn", "Отправить заявку");
				send.addEventListener("click", async () => {
					send.disabled = true;
					try {
						if (!area.value.trim()) throw new Error("Укажи мотивацию.");
						await api("/api/role-requests", "POST", { motivation: area.value.trim() });
						area.value = "";
						status.textContent = "Заявка на роль developer отправлена.";
						renderRequests();
					} catch (err) { status.textContent = String(err.message || err); }
					finally { send.disabled = false; }
				});
				form.append(label, send);
				sheet.insertBefore(form, requestsCard);
			}

			function renderRequests() {
				api("/api/role-requests").then((data) => {
					for (const child of [...requestsCard.children]) if (child !== requestsTitle) child.remove();
					const list = Array.isArray(data && data.requests) ? data.requests : [];
					if (!list.length) {
						const empty = el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "Заявок нет.");
						empty.dataset.agctlEmpty = "1";
						requestsCard.appendChild(empty);
						return;
					}
					for (const r of list) {
						const card = el("div", "agctl-card");
						card.dataset.agctlRequest = "1";
						card.appendChild(el("strong", null, r.login + " · " + r.status));
						card.appendChild(el("p", "white-space:pre-wrap;overflow-wrap:anywhere", r.motivation));
						card.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "Создана: " + r.createdAt + (r.decidedBy ? " · Решение: " + r.decidedBy : "")));
						if (me.role === "admin" && r.status === "pending") {
							const actions = el("div", "display:flex;gap:8px");
							for (const [decision, label] of [["approve", "Одобрить"], ["reject", "Отклонить"]]) {
								const btn = el("button", "agctl-btn", label);
								btn.addEventListener("click", async () => {
									btn.disabled = true;
									try {
										await api("/api/role-requests/" + encodeURIComponent(r.id) + "/" + decision, "POST", {});
										status.textContent = "Решение сохранено.";
										renderRequests();
									} catch (err) { status.textContent = String(err.message || err); btn.disabled = false; }
								});
								actions.appendChild(btn);
							}
							card.appendChild(actions);
						}
						requestsCard.appendChild(card);
					}
				}).catch((err) => { status.textContent = "Заявки: " + err.message; });
			}
			renderRequests();
		}

		function closeOverlay() {
			for (const node of document.querySelectorAll("[data-agctl-cabinet-overlay]")) node.remove();
		}

		// ── чип пользователя: штатный слот sidebar.footer.action ────────────────
		// dsh отдаёт footArea двумя слотами: footer.action (list) и settings
		// (single, занят нативным SettingsRoot). Регистрируемся в footer.action —
		// чип встаёт ровно над Settings, не затеняя shipped UI, а collapsed-
		// состояние приходит ownerProps.wide, без MutationObserver по классам.
		// Метрики ряда и кнопки списаны с нативного VOzbGW_triggerRow/trigger
		// (dsh-client-ui-settings-general), чтобы левая граница и размер чипа
		// совпадали с кнопкой Settings пиксель-в-пиксель.
		let chipNode = null;
		let agctlUserPromise = null;

		function ensureAgctlUser() {
			if (agctlUserPromise) return agctlUserPromise;
			agctlUserPromise = fetch("/api/me", { credentials: "include", headers: { Accept: "application/json" } })
				.then((res) => (res.ok ? res.json() : null))
				.then((data) => (data && data.user && data.user.login ? data.user : null))
				.catch(() => null);
			return agctlUserPromise;
		}

		// Ряд = .VOzbGW_triggerRow: calc(100% + 4px) с блик-отступом -2px;
		// в рельсе = .VOzbGW_railRow: 36px, margin 8px 0 10px.
		function applyRowMetrics(node, wide) {
			node.style.display = "flex";
			node.style.alignItems = "center";
			node.style.flex = "none";
			node.style.minWidth = "0";
			if (wide) {
				node.style.width = "calc(100% + 4px)";
				node.style.margin = "4px -2px";
				node.style.justifyContent = "flex-start";
			} else {
				node.style.width = "36px";
				node.style.margin = "8px 0 10px";
				node.style.justifyContent = "center";
			}
		}

		// Кнопка = .VOzbGW_trigger: 42px, padding 0 10px 0 8px, gap 8px, 14/22;
		// рельса = .VOzbGW_rail: 36×36 по центру.
		function applyChipMetrics(node, wide) {
			// <button> по умолчанию inline: без явного flex аватар занимает строку,
			// а имя уезжает под него. Нативный trigger получает flex из CSS-класса.
			node.style.display = "flex";
			node.style.alignItems = "center";
			node.style.boxSizing = "border-box";
			node.style.borderRadius = "var(--dsw-radius-md, 10px)";
			node.style.cursor = "pointer";
			node.style.minWidth = "0";
			node.style.color = "inherit";
			node.style.background = "transparent";
			node.style.border = "none";
			node.style.fontFamily = "inherit";
			node.style.fontSize = "14px";
			node.style.lineHeight = "22px";
			node.style.fontWeight = "400";
			node.style.overflow = "hidden";
			node.style.textAlign = "left";
			if (wide) {
				node.style.height = "42px";
				node.style.flex = "1 1 auto";
				node.style.alignItems = "center";
				node.style.gap = "8px";
				node.style.padding = "0 10px 0 8px";
				node.style.justifyContent = "flex-start";
			} else {
				node.style.width = "36px";
				node.style.height = "36px";
				node.style.flex = "none";
				node.style.justifyContent = "center";
				node.style.gap = "0";
				node.style.padding = "0";
			}
			const spans = node.querySelectorAll("span");
			if (spans[1]) spans[1].style.display = wide ? "block" : "none";
		}

		function buildChipNode(user) {
			const chip = el("button", null, null);
			chip.dataset.agctlCabinetChip = "1";
			chip.type = "button";
			chip.setAttribute("aria-label", "Профиль и настройки agctl");
			const avatar = el("span", "width:28px;height:28px;border-radius:50%;flex:none;display:flex;align-items:center;justify-content:center;background:#8a9a5b;color:#fff;font-size:11px;font-weight:700");
			const name = el("span", "flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:14px;line-height:22px");
			chip.append(avatar, name);
			chip.addEventListener("click", (ev) => {
				ev.stopPropagation();
				toggleCabinetPopup(chip, user);
			});
			return chip;
		}

		function renderChipInto(container, user, wide) {
			applyRowMetrics(container, wide);
			if (!chipNode) chipNode = buildChipNode(user);
			chipNode.__agctlUser = user;
			const displayName = user.displayName || user.name || user.login;
			const spans = chipNode.querySelectorAll("span");
			if (spans[0].textContent !== initials(displayName)) spans[0].textContent = initials(displayName);
			if (spans[1].textContent !== displayName) spans[1].textContent = displayName;
			if (chipNode.parentElement !== container) container.appendChild(chipNode);
			applyChipMetrics(chipNode, wide);
			return chipNode;
		}

		function ProfileChip(props) {
			const wide = props.wide !== false;
			const hostRef = React.useRef(null);
			React.useEffect(() => {
				const host = hostRef.current;
				if (!host) return undefined;
				applyRowMetrics(host, wide);
				let alive = true;
				ensureAgctlUser().then((user) => {
					if (alive && host.isConnected && user) renderChipInto(host, user, wide);
				});
				return () => {
					alive = false;
					if (chipNode && chipNode.parentElement === host) host.removeChild(chipNode);
				};
			}, []);
			React.useEffect(() => {
				const host = hostRef.current;
				if (!host) return;
				applyRowMetrics(host, wide);
				if (chipNode) applyChipMetrics(chipNode, wide);
			}, [wide]);
			return React.createElement("div", { ref: hostRef, "data-agctl-profile-chip-host": "1" });
		}

		function toggleCabinetPopup(anchor, me) {
			const existing = document.querySelector("[data-agctl-cabinet-popup]");
			if (existing) { existing.remove(); return; }
			const popup = el("div", "position:fixed;z-index:10000;min-width:230px;padding:6px;border-radius:12px;border:1px solid var(--dsw-alias-border-l1,#333);background:var(--dsw-specific-menu,#1c1c1c);color:var(--dsw-alias-label-primary,inherit);box-shadow:0 12px 40px rgba(0,0,0,.4);display:flex;flex-direction:column;gap:2px");
			popup.dataset.agctlCabinetPopup = "1";
			const item = (label, act) => {
				const btn = el("button", "display:flex;align-items:center;gap:10px;border:none;background:transparent;color:inherit;font:inherit;font-size:13px;padding:9px 10px;border-radius:8px;cursor:pointer;text-align:left", label);
				btn.type = "button";
				btn.addEventListener("click", () => { popup.remove(); act(); });
				return btn;
			};
			popup.append(
				item("Профиль", () => openProfileOverlay(me)),
				item("Справка", () => openHelpOverlay()),
			);
			document.body.appendChild(popup);
			const rect = anchor.getBoundingClientRect();
			popup.style.left = Math.max(8, Math.min(rect.left, window.innerWidth - popup.offsetWidth - 8)) + "px";
			popup.style.bottom = Math.max(8, window.innerHeight - rect.top + 6) + "px";
			const dismiss = (ev) => {
				if (popup.contains(ev.target) || anchor.contains(ev.target)) return;
				popup.remove();
				document.removeEventListener("mousedown", dismiss, true);
			};
			document.addEventListener("mousedown", dismiss, true);
		}

		function apply(ctx) {
			window.__agctlCabinetCtx = ctx;
			ensureStyles();
			// Штатное расширение native sidebar: регистрируемся один раз, dsh сам
			// монтирует/размонтирует компонент и передаёт wide при collapse.
			ctx.slots.inject("sidebar.footer.action", () => ctx.slots.register({
				name: "sidebar.footer.action",
				id: "agctl-profile-chip",
				order: 10,
			}, ProfileChip));
		}

		exports.inject = ["slots"];
		exports.apply = apply;
		// Test seams: рендер чипа и загрузка пользователя без React-рантайма.
		exports.renderChipInto = renderChipInto;
		exports.ensureAgctlUser = ensureAgctlUser;
		return exports;
	},
});
