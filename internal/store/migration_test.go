package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestMigrateV5DatabaseAddsSSHColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		name := []string{"001_init.sql", "002_template.sql", "003_models.sql", "004_jira_runs.sql", "005_workspace_components.sql"}[i-1]
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := raw.Exec(fmt.Sprintf("PRAGMA user_version = %d", i)); err != nil {
			t.Fatalf("set version %d: %v", i, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade from v5: %v", err)
	}
	defer db.Close()

	for _, want := range []string{"ssh_pub_key", "ssh_port"} {
		rows, err := db.Query("PRAGMA table_info(workspaces)")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			found = found || name == want
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatalf("column %s not added by migration 006", want)
		}
	}
}
