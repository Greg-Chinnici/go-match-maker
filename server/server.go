package server

import (
	"net/http"
)

func Start(addr string) error {
	return http.ListenAndServe(addr, nil)
}
