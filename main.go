package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const lifetime time.Duration = 1 * time.Hour

var database sync.Map

type CoreInstance struct {
	Address net.IPNet `json:"address"`
	Url     string    `json:"url"`
	Name    string    `json:"name"`
	Added   time.Time `json:"added"`
}

func main() {

	http.HandleFunc("/api/register", registerDevice)
	http.HandleFunc("/api/devices", listDevices)

	go cleanup()

	log.Print("Start webserver on http://0.0.0.0:80")
	http.ListenAndServe(":80", nil)
}

func registerDevice(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Please send json", 400)
		return
	}
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}

	var t struct {
		Name string `json:"name"`
		Url  string `json:"url"`
	}

	err := json.NewDecoder(r.Body).Decode(&t)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func listDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

}

func cleanup() {
	internalFunc := func(key interface{}, value interface{}) bool {
		instances := value.([]CoreInstance)
		new := []CoreInstance{}

		// Update active list
		updated := false
		for _, instance := range instances {
			if time.Since(instance.Added) < lifetime {
				new = append(new, instance)
			} else {
				updated = true
			}
		}

		// Changes with need update the store
		if len(new) == 0 {
			database.Delete(key)
		} else if updated {
			database.Store(key, new)
		}

		return true
	}

	for {
		time.Sleep(time.Second * 5)
		database.Range(internalFunc)
	}
}
