// agctl cabinet — client half (hand-written Lazy-CJS bundle).
//
// Формат повторяет собранные бандлы dsh (см. @deepseek-ai/dsh-client-ui-jobs):
// window.__ModuleLoader__.load({ id, factory(require) -> module.exports })
// с экспортами inject (имена сервисов для cordis-инъекции) и apply(ctx).
// Волна 1: панель «Кабинет» в root-scoped sidebar.panellist + страница в
// keyed slot "main" (тот же id) — ровно как в dsh-client-ui-plugin-manager.
// Данные пока заглушка; живой провод к agctl REST — волна 2 (эпик
// night-dsh-plugins). Сборка не нужна: файл пишется руками в формате хоста.
window.__ModuleLoader__.load({
	id: "@arijjia/agctl-cabinet",
	factory: (require) => {
		var module = { exports: {} };
		var exports = module.exports;
		Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
		const React = require("react");
		const h = React.createElement;

		const PANEL_ID = "agctl-cabinet";

		function CabinetIcon() {
			return h(
				"svg",
				{
					width: 16,
					height: 16,
					viewBox: "0 0 16 16",
					fill: "none",
					stroke: "currentColor",
					strokeWidth: 1.4,
					strokeLinecap: "round",
					strokeLinejoin: "round",
					"aria-hidden": true,
				},
				h("rect", { x: 2, y: 2.5, width: 12, height: 11, rx: 2 }),
				h("path", { d: "M2 6.5h12" }),
				h("path", { d: "M6.5 6.5v7" }),
			);
		}

		const styles = {
			wrap: {
				height: "100%",
				boxSizing: "border-box",
				overflow: "auto",
				padding: "24px 28px",
				fontFamily: "inherit",
				color: "var(--dsw-alias-label-primary, inherit)",
			},
			title: { margin: "0 0 4px", fontSize: 20, fontWeight: 650 },
			subtitle: {
				margin: "0 0 20px",
				fontSize: 13,
				color: "var(--dsw-alias-label-tertiary, #888)",
			},
			card: {
				border: "1px solid var(--dsw-alias-border-l1, #ddd)",
				borderRadius: 10,
				padding: "14px 16px",
				marginBottom: 16,
				maxWidth: 560,
			},
			cardTitle: { margin: "0 0 10px", fontSize: 14, fontWeight: 600 },
			row: { display: "flex", gap: 8, alignItems: "center" },
			input: {
				flex: 1,
				boxSizing: "border-box",
				padding: "6px 10px",
				fontSize: 13,
				borderRadius: 8,
				border: "1px solid var(--dsw-alias-border-l1, #ccc)",
				background: "transparent",
				color: "inherit",
			},
			button: {
				padding: "6px 12px",
				fontSize: 13,
				borderRadius: 8,
				border: "1px solid var(--dsw-alias-border-l1, #ccc)",
				background: "var(--dsw-specific-button, #eee)",
				color: "inherit",
				cursor: "pointer",
			},
			place: {
				display: "flex",
				justifyContent: "space-between",
				alignItems: "center",
				gap: 12,
				padding: "10px 12px",
				border: "1px solid var(--dsw-alias-border-l1, #ddd)",
				borderRadius: 10,
				marginBottom: 8,
				maxWidth: 560,
			},
			placeName: { fontSize: 13, fontWeight: 600 },
			placeStatus: { fontSize: 12, color: "var(--dsw-alias-label-tertiary, #888)" },
			note: { fontSize: 12, color: "var(--dsw-alias-label-tertiary, #888)", marginTop: 14 },
		};

		function CabinetPage() {
			const [url, setUrl] = React.useState("");
			const [status, setStatus] = React.useState({ state: "idle" });

			const check = async () => {
				const base = url.trim().replace(/\/+$/, "");
				if (!base) {
					setStatus({ state: "error", text: "Укажи адрес agctl-сервера" });
					return;
				}
				setStatus({ state: "checking", text: "Проверяю…" });
				try {
					const res = await fetch(base + "/api/me", {
						credentials: "include",
						headers: { Accept: "application/json" },
					});
					if (res.ok) {
						const me = await res.json().catch(() => null);
						setStatus({
							state: "ok",
							text: "Подключено: " + (me && me.login ? me.login : "ok"),
						});
					} else if (res.status === 401 || res.status === 403) {
						setStatus({ state: "warn", text: "Сервер отвечает, но сессии нет — войди в кабинет agctl" });
					} else {
						setStatus({ state: "warn", text: "Сервер отвечает: HTTP " + res.status });
					}
				} catch (e) {
					setStatus({ state: "error", text: "Не отвечает: " + String((e && e.message) || e) });
				}
			};

			const places = [
				{ name: "demo-1c", status: "заглушка — волна 2 подключит agctl REST" },
				{ name: "ivanova-lms", status: "заглушка" },
				{ name: "cit-dev", status: "заглушка" },
			];

			return h(
				"div",
				{ style: styles.wrap },
				h("h1", { style: styles.title }, "Кабинет agctl"),
				h(
					"p",
					{ style: styles.subtitle },
					"Рабочие места с агентами DeepSeek Harness. Дефолтный чат этой среды — базовый уровень платформы (Tier 0).",
				),
				h(
					"div",
					{ style: styles.card },
					h("h2", { style: styles.cardTitle }, "Подключение к agctl"),
					h(
						"div",
						{ style: styles.row },
						h("input", {
							style: styles.input,
							placeholder: "https://agctl.example.org (или http://127.0.0.1:8929)",
							value: url,
							onChange: (e) => setUrl(e.target.value),
							onKeyDown: (e) => {
								if (e.key === "Enter") check();
							},
						}),
						h("button", { style: styles.button, onClick: check }, "Проверить"),
					),
					status.state !== "idle" &&
						h(
							"p",
							{ style: { ...styles.note, marginTop: 10 } },
							(status.state === "ok" ? "✓ " : status.state === "error" ? "✗ " : "… ") + status.text,
						),
				),
				h("h2", { style: styles.cardTitle }, "Рабочие места"),
				places.map((p) =>
					h(
						"div",
						{ style: styles.place, key: p.name },
						h("span", { style: styles.placeName }, p.name),
						h("span", { style: styles.placeStatus }, p.status),
					),
				),
				h(
					"p",
					{ style: styles.note },
					"Волна 1 эпика night-dsh-plugins: панель и экран-каркас. Список и создание мест — живой agctl REST (волна 2).",
				),
			);
		}

		const inject = ["slots", "layout"];

		/**
		 * Contribute the «Кабинет» entry to the sidebar and the page it opens.
		 * @param ctx - the browser plugin context.
		 */
		function apply(ctx) {
			ctx.slots.inject("main", () =>
				ctx.slots.register(
					{
						name: "main",
						key: PANEL_ID,
						inject: () => ({}),
					},
					CabinetPage,
				),
			);
			ctx.slots.inject("sidebar.panellist", () =>
				ctx.slots.register(
					{
						name: "sidebar.panellist",
						id: PANEL_ID,
						order: 25,
						label: "Кабинет",
					},
					CabinetIcon,
				),
			);
		}

		exports.inject = inject;
		exports.apply = apply;
		exports.PANEL_ID = PANEL_ID;
		return module.exports;
	},
});
