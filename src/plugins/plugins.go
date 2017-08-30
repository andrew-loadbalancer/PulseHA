package plugins

import (
	log "github.com/Sirupsen/logrus"
	"github.com/Syleron/Pulse/src/structures"
	"github.com/Syleron/Pulse/src/utils"
	"path"
	"path/filepath"
	"plugin"
	"strconv"
)

/**
 *
 **/
func Load() ([]structures.PluginHC, error) {
	var modules []structures.PluginHC

	utils.CreateFolder("./plugins")

	evtGlob := path.Join("./plugins", "/*.so")
	evt, err := filepath.Glob(evtGlob)

	if err != nil {
		return modules, err
	}

	var plugins []*plugin.Plugin

	for _, pFile := range evt {
		if plug, err := plugin.Open(pFile); err == nil {
			plugins = append(plugins, plug)
		}
	}

	for _, p := range plugins {
		symEvt, err := p.Lookup("PluginHC")

		if err != nil {
			log.Errorf("Plugin has no pluginType symbol: %v", err)
			continue
		}

		e, ok := symEvt.(structures.PluginHC)

		if !ok {
			log.Error("Plugin is not of an PluginHC interface type")
			continue
		}

		modules = append(modules, e)
	}

	if len(modules) > 0 {
		var pluginNames string = ""

		for _, plgn := range modules {
			pluginNames += plgn.Name() + "(v" + strconv.FormatFloat(plgn.Version(), 'f', -1, 32) + ") "
		}

		log.Infof("Plugins loaded (%v): %v", len(modules), pluginNames)
	}

	return modules, nil
}