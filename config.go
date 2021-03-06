package main

type Config struct {
	Port           string `json:"port"`
	Requests       int    `json:"requests"`
	Workers        int    `json:"workers"`
	HTTPTimeout    int
	IsSlave        bool
	Quiet          bool
	UserAgent      string
	ListenBindAddr string
	Urls           []string
	Hosts          []string
}
