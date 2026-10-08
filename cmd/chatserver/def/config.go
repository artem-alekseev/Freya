package def

import (
	"github.com/ubis/Freya/share/conf"
	"github.com/ubis/Freya/share/directory"
	"github.com/ubis/Freya/share/log"
)

type Config struct {
	Port       int
	MasterIp   string
	MasterPort int
	ServerID   int
}

// Read loads the standalone ChatNode configuration.
func (c *Config) Read() {
	log.Info("Reading configuration...")
	c.Port = 38112
	c.MasterIp = "127.0.0.1"
	c.MasterPort = 9002
	c.ServerID = 1

	location := directory.Root() + "/cfg/chatserver.ini"
	if err := conf.Open(location); err != nil {
		log.Warningf("Unable to open ChatNode configuration, using defaults: %s", err)
		return
	}

	c.Port = conf.GetInt("network", "port", c.Port)
	c.MasterIp = conf.GetString("master", "ip", c.MasterIp)
	c.MasterPort = conf.GetInt("master", "port", c.MasterPort)
	c.ServerID = conf.GetInt("server", "id", c.ServerID)
}
