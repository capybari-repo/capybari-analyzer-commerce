// Command capybari-commerce runs this capability on its own.
package main

import (
	commerce "github.com/capybari-repo/capybari-analyzer-commerce"
	"github.com/capybari-repo/capybari-core/standalone"
)

var version = "dev"

func main() { standalone.Main(version, commerce.New()) }
