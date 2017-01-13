package main

type Config struct {
	Port      int `json:"port"`
	Requests  int `json:"requests"`
	Workers   int `json:"workers"`
	IsSlave   bool
	UserAgent string
	Urls      []string
	Hosts     []string
}
