package main

import (
	"flag"
	"io/ioutil"
	"time"

	"github.com/contactless/wbgo"
)

const (
	DRIVER_CLIENT_ID = "wb-mqtt-scpi"
)

func main() {
	configPath := flag.String("config", "/etc/wb-mqtt-scpi.conf", "config path")
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker url")
	debug := flag.Bool("debug", false, "Enable debugging")
	flag.Parse()

	if *debug {
		wbgo.SetDebuggingEnabled(true)
	}

	confBytes, err := ioutil.ReadFile(*configPath)
	if err != nil {
		wbgo.Error.Fatalf("can't load config: %v", err)
	}
	config, err := ParseDriverConfig(confBytes)
	if err != nil {
		wbgo.Error.Fatalf("can't parse config: %v", err)
	}

	model := NewModel(DefaultCommanderFactory(connect), config)
	mqttClient := wbgo.NewPahoMQTTClient(*broker, DRIVER_CLIENT_ID, false)
	driver := wbgo.NewDriver(model, mqttClient)
	// NOTE: this is not 'real' poll interval
	// The model polls the device continuously
	driver.SetPollInterval(50 * time.Millisecond)
	if err := driver.Start(); err != nil {
		wbgo.Error.Fatalf("failed to start the driver: %v", err)
	}
	for {
		time.Sleep(1 * time.Second)
	}

	// conn, err := connect("192.168.255.209:10010")
	// if err != nil {
	// 	log.Panicf("connect failed")
	// }
	// c := textproto.NewConn(conn)
	// id, err := c.Cmd("*IDN?")
	// if err != nil {
	// 	log.Panicf("Cmd() failed")
	// }
	// c.StartResponse(id)
	// text, err := c.ReadLine()
	// fmt.Printf("text=%#v\n", text)
	// c.EndResponse(id)
}
