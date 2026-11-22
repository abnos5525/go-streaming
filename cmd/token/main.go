package main

import (
	"fmt"
	"os"
	"time"

	"github.com/abnos5525/go-streaming/internal/auth"
	"github.com/abnos5525/go-streaming/internal/config"
)

func main() {
	config.Load()
	token, err := auth.Sign(os.Getenv("AUTH_SECRET"), time.Hour, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(token)
}
