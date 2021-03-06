package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/spf13/viper"
)

const lifetime time.Duration = 1 * time.Hour
const env_forwarded string = "USE_FORWARDED_FOR"

var database sync.Map
var gProxy bool

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

	viper.SetDefault(env_forwarded, false)
	viper.AutomaticEnv()

	gProxy = viper.GetBool(env_forwarded)

	http.HandleFunc("/api/register", registerDevice)
	http.HandleFunc("/api/devices", listDevices)

	go cleanup()

	log.Printf("Start webserver on http://0.0.0.0:80 (Proxy-Support: %t)", gProxy)
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
	ip := getIpAddress(r)
	log.Printf("Get register request from %s", ip.String())

	// load data
	var record *DataRecord

	data, loaded := database.Load(ip.String())
	if loaded {
		record = data.(*DataRecord)
	} else {
		record = &DataRecord{sync.RWMutex{}, ip, []CoreInstance{}}
		defer database.Store(ip.String(), record)
	}

	record.Mutex.Lock()
	defer record.Mutex.Unlock()

	// Add instance
	instance := CoreInstance{t.Url, t.Name, time.Now()}

	// Filter old out
	for i := len(record.Instances) - 1; i >= 0; i-- {
		if record.Instances[i].Url == instance.Url {
			record.Instances = append(record.Instances[:i], record.Instances[i+1:]...)
		}
	}
	record.Instances = append(record.Instances, instance)

	w.WriteHeader(http.StatusOK)
}

func listDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	ip := getIpAddress(r)
	log.Printf("Get list request from %s", ip.String())

	// Instances
	data, ok := database.Load(ip.String())
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
	if isProxy && gProxy {
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
	if ip.To4() == nil {
		_, network, _ = net.ParseCIDR(ip.String() + "/64")
	} else {
		_, network, _ = net.ParseCIDR(ip.String() + "/32")
	}

	return *network
}

func cleanup() {
	internalFunc := func(key interface{}, value interface{}) bool {
		record := value.(*DataRecord)

		// RWLock for edit data
		record.Mutex.Lock()
		defer record.Mutex.Unlock()

		// Update active list
		for i := len(record.Instances) - 1; i >= 0; i-- {
			if time.Since(record.Instances[i].Added) > lifetime {
				record.Instances = append(record.Instances[:i], record.Instances[i+1:]...)
			}
		}

		// Changes with need update the store
		if len(record.Instances) == 0 {
			database.Delete(key)
		}

		return true
	}

	for {
		time.Sleep(time.Minute * 5)
		database.Range(internalFunc)
	}
}
