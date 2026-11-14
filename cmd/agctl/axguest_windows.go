//go:build windows

package main

import (
	"errors"

	"google.golang.org/grpc"
)

// newGuestServer — на Windows guest-сервисов Substrate нет (раннер AX живёт
// только в linux-песочнице).
func newGuestServer(string) (*grpc.Server, func(), error) {
	return nil, nil, errors.New("guest-сервисы Substrate доступны только на Linux")
}
