/**
 * Node half of the agctl chat-brand plugin. The browser half in `lib/client.js`
 * owns every contribution; this entry exists so the package appears as an
 * ordinary Loader row (same pattern as @arijjia/agctl-cabinet).
 * @module @arijjia/agctl-chat-brand
 */
/** Loader-visible no-op body; the browser half carries the feature. */
function apply() {}
export { apply };
