package dockerrt

import "testing"

func TestHasActiveSSHProcess(t *testing.T) {
	for _, tc := range []struct {
		name      string
		processes [][]string
		want      bool
	}{
		{name: "no sshd", processes: [][]string{{"node", "dsh web"}}, want: false},
		{name: "command mentions sshd", processes: [][]string{{"bash -lc echo sshd: root@notty"}}, want: false},
		{name: "listener only", processes: [][]string{{"sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups"}}, want: false},
		{name: "notty session", processes: [][]string{{"17", "sshd: root@notty"}}, want: true},
		{name: "terminal session", processes: [][]string{{"18", "sshd: root@pts/0"}}, want: true},
		{name: "preauth child", processes: [][]string{{"19", "sshd: root [priv]"}}, want: true},
		{name: "listener and session", processes: [][]string{{"1", "sshd: /usr/sbin/sshd [listener]"}, {"20", "sshd: root@notty"}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasActiveSSHProcess(tc.processes); got != tc.want {
				t.Fatalf("hasActiveSSHProcess(%v) = %t, want %t", tc.processes, got, tc.want)
			}
		})
	}
}
