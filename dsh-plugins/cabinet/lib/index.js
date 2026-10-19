/**
 * Node half of the agctl cabinet plugin. The browser half in `lib/client.js`
 * owns every contribution; this entry exists so the package appears as an
 * ordinary Loader row (same pattern as @deepseek-ai/dsh-client-ui-jobs).
 * @module @arijjia/agctl-cabinet
 */
/** Loader-visible no-op body; the browser half carries the feature. */
function apply() {}
export { apply };
