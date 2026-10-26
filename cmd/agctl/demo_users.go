package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/store"
)

// store.ErrNotFound (wrapped sentinel) — логина нет в базе.
var errNoLogin = store.ErrNotFound

type demoAccount struct {
	login    string
	password string
	role     string
}

// These credentials are documented as local-pilot-only. The command is explicit
// and never runs from serve/make up; an existing login is never modified.
var demoAccounts = []demoAccount{
	{login: "demo-user", password: "DemoUser-2026!", role: "user"},
	{login: "demo-developer", password: "DemoDeveloper-2026!", role: "developer"},
	{login: "demo-admin", password: "DemoAdmin-2026!", role: "admin"},
}

func seedDemoAccounts(home string) (int, error) {
	absHome, err := requireHome(home)
	if err != nil {
		return 0, err
	}
	db, err := store.Open(filepath.Join(absHome, "agctl.db"))
	if err != nil {
		return 0, err
	}
	defer db.Close()

	count, err := db.CountUsers()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, fmt.Errorf("сначала зарегистрируй основную учётку: первый пользователь становится admin")
	}

	created := 0
	for _, account := range demoAccounts {
		if _, err := db.UserByLogin(account.login); err == nil {
			continue // не ротируем пароль и не меняем роль существующей учётки
		} else if !errors.Is(err, errNoLogin) {
			return created, err
		}
		hash, err := auth.HashPassword(account.password)
		if err != nil {
			return created, fmt.Errorf("hash demo password for %s: %w", account.login, err)
		}
		if _, err := db.CreateUser(account.login, hash, account.role); err != nil {
			return created, fmt.Errorf("create demo user %s: %w", account.login, err)
		}
		created++
	}
	return created, nil
}

func cmdSeedDemoUsers(args []string) {
	fsSet := flag.NewFlagSet("seed-demo-users", flag.ExitOnError)
	home := fsSet.String("home", defaultHome(), "каталог данных agctl")
	_ = fsSet.Parse(args)
	created, err := seedDemoAccounts(*home)
	if err != nil {
		log.Fatal("seed-demo-users: ", err)
	}
	fmt.Printf("создано тестовых аккаунтов: %d (существующие не изменялись)\n", created)
}
