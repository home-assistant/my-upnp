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
	Url   string    `json:"url"`
	Name  string    `json:"name"`
	Added time.Time `json:"-"`
}

type DataRecord struct {
	Mutex     sync.RWMutex
	Network   net.IPNet
	Instances []CoreInstance
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

	// Init record
	ip := getIpAddress(r)

	// load data
	record := &DataRecord{sync.RWMutex{}, ip, []CoreInstance{}}
	data, isNew := database.LoadOrStore(ip, record)

	if !isNew {
		record = data.(*DataRecord)
	}
	record.Mutex.Lock()
	defer record.Mutex.Unlock()

	// Add instance
	instance := CoreInstance{t.Url, t.Name, time.Now()}
	record.Instances = append(record.Instances, instance)

	w.WriteHeader(http.StatusOK)
}

func listDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	ip := getIpAddress(r)

	// Instances
	data, ok := database.Load(ip)
	if !ok {
		return
	}
	record := data.(*DataRecord)

	record.Mutex.RLock()
	defer record.Mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(record.Instances); err != nil {
		log.Panicf(err.Error())
	}
}

func getIpAddress(r *http.Request) net.IPNet {
	_, isProxy := r.Header["X-Forwarded-For"]

	// Get IP
	var IPAddress string
	if isProxy {
		IPAddress = r.Header.Get("X-Forwarded-For")
	} else {
		IPAddress, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	// Parse IP
	ip := net.ParseIP(IPAddress)
	if ip == nil {
		log.Panicf("Can't parse %s", IPAddress)
	}

	// Generate the key based on IPv6 or IPv4
	var network *net.IPNet
	if ip.To16() != nil {
		_, network, _ = net.ParseCIDR(ip.String() + "/64")
	} else {
		_, network, _ = net.ParseCIDR(ip.String() + "/32")
	}

	return *network
}

func cleanup() {
	internalFunc := func(key interface{}, value interface{}) bool {
		record := value.(*DataRecord)
		new := []CoreInstance{}

		// RWLock for edit data
		record.Mutex.Lock()
		defer record.Mutex.Unlock()

		// Update active list
		updated := false
		for _, instance := range record.Instances {
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
			database.Store(key, record)
		}

		return true
	}

	for {
		time.Sleep(time.Second * 5)
		database.Range(internalFunc)
	}
}
