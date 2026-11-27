package dockerrt

// hostuser.go — контейнер рабочего места под хостовым пользователем (локальный
// режим на Linux, ChatSpec.User). Процесс получает запись в /etc/passwd и
// /etc/group (без неё ssh, git и whoami не знают пользователя: «No user
// exists for uid»), а файлы dsh-home, оставшиеся от прежних запусков под
// root, перед стартом возвращаются хозяину одноразовым контейнером — без sudo
// на хосте.

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// imagePasswd, imageGroup — системные записи образа agctl-dsh (node:22-
// bookworm-slim); запись пользователя добавляется второй строкой, чтобы
// getpwuid нашёл её раньше совпадающего node (uid 1000).
const imagePasswd = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
bin:x:2:2:bin:/bin:/usr/sbin/nologin
sys:x:3:3:sys:/dev:/usr/sbin/nologin
sync:x:4:65534:sync:/bin:/bin/sync
games:x:5:60:games:/usr/games:/usr/sbin/nologin
man:x:6:12:man:/var/cache/man:/usr/sbin/nologin
lp:x:7:7:lp:/var/spool/lpd:/usr/sbin/nologin
mail:x:8:8:mail:/var/mail:/usr/sbin/nologin
news:x:9:9:news:/var/spool/news:/usr/sbin/nologin
uucp:x:10:10:uucp:/var/spool/uucp:/usr/sbin/nologin
proxy:x:13:13:proxy:/bin:/usr/sbin/nologin
www-data:x:33:33:www-data:/var/www:/usr/sbin/nologin
backup:x:34:34:backup:/var/backups:/usr/sbin/nologin
list:x:38:38:Mailing List Manager:/var/list:/usr/sbin/nologin
irc:x:39:39:ircd:/run/ircd:/usr/sbin/nologin
_apt:x:42:65534::/nonexistent:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
node:x:1000:1000::/home/node:/bin/bash
sshd:x:100:65534::/run/sshd:/usr/sbin/nologin
redis:x:101:102::/var/lib/redis:/usr/sbin/nologin
`

const imageGroup = `root:x:0:
daemon:x:1:
bin:x:2:
sys:x:3:
adm:x:4:
tty:x:5:
disk:x:6:
lp:x:7:
mail:x:8:
news:x:9:
uucp:x:10:
man:x:12:
proxy:x:13:
kmem:x:15:
dialout:x:20:
fax:x:21:
voice:x:22:
cdrom:x:24:
floppy:x:25:
tape:x:26:
sudo:x:27:
audio:x:29:
dip:x:30:
www-data:x:33:
backup:x:34:
operator:x:37:
list:x:38:
irc:x:39:
src:x:40:
shadow:x:42:
utmp:x:43:
video:x:44:
sasl:x:45:
plugdev:x:46:
staff:x:50:
games:x:60:
users:x:100:
nogroup:x:65534:
node:x:1000:
_ssh:x:101:
redis:x:102:
`

// imageUserAPI — с какого guest API образ работает не под root (кэш npx на
// запись любому uid, sshd только под root).
const imageUserAPI = 3

// imageRunsAsUser — образ ref умеет работать под пользователем хоста (метка
// io.agctl.guest.api ≥ imageUserAPI). Старый образ — под root, как раньше.
func (rt *Runtime) imageRunsAsUser(ctx context.Context, ref string) bool {
	insp, err := rt.cli.ImageInspect(ctx, ref)
	if err != nil || insp.Config == nil {
		return false
	}
	api, _ := strconv.Atoi(insp.Config.Labels["io.agctl.guest.api"])
	return api >= imageUserAPI
}

// parseUser — "uid:gid" → числа.
func parseUser(user string) (uid, gid int, err error) {
	u, g, ok := strings.Cut(user, ":")
	if !ok {
		return 0, 0, fmt.Errorf("пользователь контейнера %q: нужно uid:gid", user)
	}
	if uid, err = strconv.Atoi(u); err != nil || uid < 0 {
		return 0, 0, fmt.Errorf("пользователь контейнера %q: uid", user)
	}
	if gid, err = strconv.Atoi(g); err != nil || gid < 0 {
		return 0, 0, fmt.Errorf("пользователь контейнера %q: gid", user)
	}
	return uid, gid, nil
}

// userFiles — /etc/passwd и /etc/group образа с записью «dsh» для uid:gid
// (домашний каталог — /workspace, как HOME процесса агента).
func userFiles(uid, gid int) (passwd, group string) {
	lines := strings.SplitAfter(imagePasswd, "\n")
	entry := fmt.Sprintf("dsh:x:%d:%d:agctl:/workspace:/bin/bash\n", uid, gid)
	if uid == 0 {
		return imagePasswd, imageGroup
	}
	passwd = lines[0] + entry + strings.Join(lines[1:], "")
	group = imageGroup
	if !strings.Contains("\n"+imageGroup, fmt.Sprintf(":x:%d:\n", gid)) {
		group += fmt.Sprintf("dsh:x:%d:\n", gid)
	}
	return passwd, group
}

// writeUserFiles кладёт passwd и group в dir и возвращает bind-маунты
// (только чтение).
func writeUserFiles(dir string, uid, gid int) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	passwd, group := userFiles(uid, gid)
	pw, gr := filepath.Join(dir, "passwd"), filepath.Join(dir, "group")
	if err := os.WriteFile(pw, []byte(passwd), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(gr, []byte(group), 0o644); err != nil {
		return nil, err
	}
	return []string{pw + ":/etc/passwd:ro", gr + ":/etc/group:ro"}, nil
}

// chownTree возвращает дерево dir хозяину uid:gid, если в нём есть чужие
// файлы (остались от запусков под root): одноразовый контейнер образа image
// под root делает chown -R. Ошибка не фатальна — место стартует, причина в
// логе.
func (rt *Runtime) chownTree(ctx context.Context, image, dir string, uid, gid int) {
	if dir == "" || !foreignOwned(dir, uid) {
		return
	}
	owner := fmt.Sprintf("%d:%d", uid, gid)
	res, err := rt.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      image,
			User:       "0:0",
			Entrypoint: []string{"chown", "-R", owner, "/fix"},
			Labels:     map[string]string{labelWorkspace: "chown"},
		},
		HostConfig: &container.HostConfig{Binds: []string{dir + ":/fix"}},
	})
	if err != nil {
		log.Printf("[dockerrt] владелец %s → %s: %v", dir, owner, err)
		return
	}
	defer func() {
		_, _ = rt.cli.ContainerRemove(context.Background(), res.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := rt.cli.ContainerStart(ctx, res.ID, client.ContainerStartOptions{}); err != nil {
		log.Printf("[dockerrt] владелец %s → %s: %v", dir, owner, err)
		return
	}
	wait := rt.cli.ContainerWait(ctx, res.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case r := <-wait.Result:
		if r.StatusCode != 0 {
			log.Printf("[dockerrt] владелец %s → %s: chown вышел с кодом %d", dir, owner, r.StatusCode)
			return
		}
		log.Printf("[dockerrt] файлы %s, оставшиеся от root, возвращены владельцу %s", dir, owner)
	case err := <-wait.Error:
		log.Printf("[dockerrt] владелец %s → %s: %v", dir, owner, err)
	}
}
