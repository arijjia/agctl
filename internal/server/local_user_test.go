package server

import "testing"

func TestLocalContainerUser(t *testing.T) {
	for _, c := range []struct {
		goos, override string
		uid, gid       int
		want           string
	}{
		{"linux", "", 1234, 1235, "1234:1235"},
		{"linux", "", 0, 0, ""}, // agctl под root — контейнер как есть
		{"darwin", "", 501, 20, ""},
		{"windows", "", -1, -1, ""},
		{"linux", "root", 1234, 1235, ""},
		{"darwin", "host", 501, 20, "501:20"},
		{"darwin", "1000:1000", 501, 20, "1000:1000"},
		{"linux", "мусор", 1234, 1235, "1234:1235"},
	} {
		if got := localContainerUser(c.goos, c.override, c.uid, c.gid); got != c.want {
			t.Errorf("%s %q %d:%d = %q, ждали %q", c.goos, c.override, c.uid, c.gid, got, c.want)
		}
	}
}
