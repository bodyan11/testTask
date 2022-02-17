package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/naoina/toml"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

var cache = New(0, 0)

type Config struct {
	Ip        string
	Address   string
	CacheSize int
}

type Cache struct {
	sync.RWMutex
	defaultExpiration time.Duration
	cleanupInterval   time.Duration
	items             map[string]Item
	cacheSize         int
}

type Item struct {
	Value   []byte
	Created time.Time
}

func New(defaultExpiration, cleanupInterval time.Duration) *Cache {

	items := make(map[string]Item)

	cache := Cache{
		items:             items,
		defaultExpiration: defaultExpiration,
		cleanupInterval:   cleanupInterval,
	}

	return &cache

}

func (c *Cache) Set(key string, value []byte) {
	c.Lock()

	defer c.Unlock()

	c.items[key] = Item{
		Value:   value,
		Created: time.Now(),
	}
}

func (c *Cache) Get(key string) ([]byte, time.Time, bool) {
	c.RLock()

	defer c.RUnlock()

	item, found := c.items[key]

	if !found {
		return nil, time.Now(), false
	}

	return item.Value, item.Created, true
}

func (c *Cache) Delete(key string) error {
	c.Lock()

	defer c.Unlock()

	if _, found := c.items[key]; !found {
		return errors.New("key not found")
	}

	delete(c.items, key)

	return nil
}

func delOldCache() error {
	var (
		min int64
		key string
	)

	i := 0

	for k, v := range cache.items {

		if i == 0 {
			min = v.Created.UnixMilli()
			key = k
		}

		if v.Created.UnixMilli() <= min {
			min = v.Created.UnixMilli()
			key = k
		}

		i++
	}

	if len(cache.items) > 1 {
		cache.Delete(key)
	}

	return nil
}

func readConfig() (*Config, error) {
	var config *Config

	configFile, err := os.Open("config.toml")
	if err != nil {
		panic(err)
	}

	defer configFile.Close()

	err = toml.NewDecoder(configFile).Decode(&config)
	if err != nil {
		return nil, errors.New("config not found")
	}

	return config, nil
}

func HandlerProxy(w http.ResponseWriter, r *http.Request) {
	config, err := readConfig()
	if err != nil {
		panic(err)
	}

	_, _, exists := cache.Get(config.Address)

	if exists == false {

		client := &http.Client{}
		req, _ := http.NewRequest("GET", "https://"+config.Address+"/", nil)

		req.Header.Set("Host", config.Address)
		req.Header.Set("Refer", config.Ip)

		resp, _ := client.Do(req)

		if err != nil {
			log.Fatal(err)
		}

		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			log.Fatal(err)
		}

		pageSize := binary.Size(body)

		a := cache.cacheSize

		newSize := pageSize / 1024

		cache.cacheSize = a + newSize

		cache.Set(config.Address, body)

		fmt.Println(newSize, cache.cacheSize)

		if cache.cacheSize >= config.CacheSize {

			err := delOldCache()

			if err != nil {
				log.Fatal(err)
			}
		}

	}

	page, _, exists := cache.Get(config.Address)

	w.WriteHeader(200)
	w.Write(page)

	return
}

func main() {
	config, err := readConfig()
	if err != nil {
		panic(err)
	}

	router := mux.NewRouter()
	router.HandleFunc("/", HandlerProxy)
	http.Handle("/", router)

	fmt.Println("Server is listening...")
	http.ListenAndServe(config.Ip, nil)
}
