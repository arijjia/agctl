/**
 * Node half of the agctl workspace-console plugin. The browser half in
 * `lib/client.js` owns every contribution; this entry exists so the package
 * appears as an ordinary Loader row (same pattern as @arijjia/agctl-cabinet).
 * Без экспорта `apply` cordis не считает модуль плагином: строка Loader не
 * активируется и в логе контейнера появляется «failed to import».
 * @module @arijjia/agctl-workspace-console
 */
/** Loader-visible no-op body; the browser half carries the feature. */
function apply() {}
export { apply };
