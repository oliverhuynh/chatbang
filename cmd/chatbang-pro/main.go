package main

import (
	"os"

	"github.com/KaraBala10/chatbang-pro/internal/app"
)

var version = "dev"

func main() {
	app.Run(version, os.Args)
}
