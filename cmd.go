package main

//github.com/urfave/cli
import (
	"os"

	"github.com/urfave/cli"
)

func parseFlags() Config {
	var config Config
	var help bool = true

	app := cli.NewApp()
	app.Name = "HTTP(s) Load Testing"
	app.Version = "0.1.0"
	app.Flags = []cli.Flag{
		cli.IntFlag{
			Name:  "port, p",
			Value: 9055,
			Usage: "listen port master/slave",
		},
		cli.IntFlag{
			Name:  "requests, r",
			Value: 10,
			Usage: "number of requests",
		},
		cli.IntFlag{
			Name:  "concurrency, c",
			Value: 10,
			Usage: "number of concurrent requests",
		},
		cli.StringFlag{
			Name:  "bind-address",
			Usage: "bind local address",
		},
		cli.BoolFlag{
			Name:  "slave",
			Usage: "sets in slave mode",
		},
		cli.StringSliceFlag{
			Name: "url, u",
		},
		cli.StringSliceFlag{
			Name: "slave-host",
		},
		cli.StringFlag{
			Name:  "user-agent",
			Value: "load",
			Usage: "sets user-agent",
		},
	}

	app.Action = func(c *cli.Context) error {
		config.Port = c.Int("port")
		config.Urls = c.StringSlice("url")
		config.Hosts = c.StringSlice("slave-host")
		config.Requests = c.Int("requests")
		config.Workers = c.Int("concurrency")
		config.IsSlave = c.Bool("slave")
		config.UserAgent = c.String("user-agent")

		help = false

		return nil
	}

	app.Run(os.Args)

	if help {
		os.Exit(0)
	}

	return config
}
