package server

import (
	"net/http/httptest"
	"testing"
)

func TestStripAgctlCookiesKeepsOnlyUpstreamCookies(t *testing.T) {
	req := httptest.NewRequest("GET", "http://platform.example/chat/", nil)
	req.Header.Set("Cookie", "agctl_session=platform-test; dsh-auth-user=runtime-test; theme=light; agctl_connector=connector-test")

	stripAgctlCookies(req)
	got := map[string]string{}
	for _, cookie := range req.Cookies() {
		got[cookie.Name] = cookie.Value
	}
	if len(got) != 2 || got["dsh-auth-user"] != "runtime-test" || got["theme"] != "light" {
		t.Fatalf("upstream cookies = %#v; want DSH+unrelated cookies only", got)
	}
	if _, ok := got["agctl_session"]; ok {
		t.Fatal("platform session cookie leaked to DSH upstream")
	}
	if _, ok := got["agctl_connector"]; ok {
		t.Fatal("agctl connector cookie leaked to DSH upstream")
	}
}
