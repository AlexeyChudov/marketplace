package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr  string
	JWTSecret string
}

// Load reads command-line flags and environment variables. --addr takes
// precedence over PORT; JWT_SECRET is required in every environment.
func Load(args []string) (Config, error) {
	flags := flag.NewFlagSet("marketplace-api", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addrFlag := flags.String("addr", "", "HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	addr := *addrFlag
	if addr == "" {
		if port := os.Getenv("PORT"); port != "" {
			portNumber, err := strconv.Atoi(port)
			if err != nil || portNumber < 1 || portNumber > 65535 {
				return Config{}, fmt.Errorf("PORT must be a number from 1 to 65535")
			}
			addr = ":" + port
		} else {
			addr = ":8080"
		}
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	return Config{HTTPAddr: addr, JWTSecret: secret}, nil
}
