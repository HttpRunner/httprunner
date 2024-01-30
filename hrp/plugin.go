package hrp

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/httprunner/funplugin"
	"github.com/httprunner/funplugin/myexec"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/httprunner/httprunner/v4/hrp/internal/code"
	"github.com/httprunner/httprunner/v4/hrp/internal/env"
	"github.com/httprunner/httprunner/v4/hrp/internal/sdk"
)

const (
	PluginGoBuiltFile          = "debugtalk.so"      // built from go official plugin
	PluginHashicorpGoBuiltFile = "debugtalk.bin"     // built from hashicorp go plugin
	PluginGoSourceFile         = "debugtalk.go"      // golang function plugin source file
	PluginGoSourceGenFile      = "debugtalk_gen.go"  // generated for hashicorp go plugin
	PluginPySourceFile         = "debugtalk.py"      // python function plugin source file
	PluginPySourceGenFile      = ".debugtalk_gen.py" // generated for hashicorp python plugin
)

const projectInfoFile = "proj.json" // used for ensuring root project

var (
	pluginMap   sync.Map // used for reusing plugin instance
	pluginMutex sync.RWMutex
)

func initPlugin(path, venv string, logOn bool) (plugin funplugin.IPlugin, err error) {
	// plugin file not found
	if path == "" {
		return nil, nil
	}
	pluginPath, err := locatePlugin(path)
	if err != nil {
		log.Warn().Str("path", path).Msg("locate plugin failed")
		return nil, nil
	}

	pluginMutex.Lock()
	defer pluginMutex.Unlock()
	var KeygenPyPluginPath string
	if strings.HasSuffix(pluginPath, ".py") {
		// register funppy plugin
		KeygenPyPluginPath := filepath.Join(filepath.Dir(pluginPath), PluginPySourceGenFile)
	// reuse plugin instance if it already initialized
	if p, ok := pluginMap.Load(KeygenPyPluginPath); ok {
		return p.(funplugin.IPlugin), nil
	}

	pluginOptions := []funplugin.Option{funplugin.WithDebugLogger(logOn)}

	if strings.HasSuffix(pluginPath, ".py") {
		// register funppy plugin
		genPyPluginPath := filepath.Join(filepath.Dir(pluginPath), PluginPySourceGenFile)
		err = BuildPlugin(pluginPath, genPyPluginPath)
		if err != nil {
			log.Error().Err(err).Str("path", pluginPath).Msg("build plugin failed")
			return nil, err
		}
		pluginPath = genPyPluginPath

		packages := []string{"funppy"}
		python3, err := myexec.EnsurePython3Venv(venv, packages...)
		if err != nil {
			log.Error().Err(err).
				Interface("packages", packages).
				Msg("python3 venv is not ready")
			return nil, errors.Wrap(code.InvalidPython3Venv, err.Error())
		}
		pluginOptions = append(pluginOptions, funplugin.WithPython3(python3))
	}

	// found plugin file
	plugin, err = funplugin.Init(pluginPath, pluginOptions...)
	if err != nil {
		log.Error().Str("path", pluginPath).Msg("init plugin failed")
		err = errors.Wrap(code.InitPluginFailed, err.Error())
		return
	}

	// add plugin instance to plugin map
	pluginMap.Store(pluginPath, plugin)

	// report event for initializing plugin
	params := map[string]interface{}{
		"type":   plugin.Type(),
		"result": "success",
	}
	if err != nil {
		params["result"] = "failed"
	}
	go sdk.SendGA4Event("init_plugin", params)

	return
}

func locatePlugin(path string) (pluginPath string, err error) {
	// priority: hashicorp plugin (debugtalk.bin > debugtalk.py) > go plugin (debugtalk.so)

	pluginPath, err = locateFile(path, PluginHashicorpGoBuiltFile)
	if err == nil {
		return
	}

	pluginPath, err = locateFile(path, PluginPySourceFile)
	if err == nil {
		return
	}

	pluginPath, err = locateFile(path, PluginGoBuiltFile)
	if err == nil {
		return
	}

	return "", errors.New("plugin file not found")
}

// locateFile searches destFile upward recursively until system root dir
// if not found, then searches in hrp executable dir
func locateFile(startPath string, destFile string) (pluginPath string, err error) {
	stat, err := os.Stat(startPath)
	if os.IsNotExist(err) {
		return "", errors.Wrap(err, "start path not exists")
	}

	var startDir string
	if stat.IsDir() {
		startDir = startPath
	} else {
		startDir = filepath.Dir(startPath)
	}
	startDir, _ = filepath.Abs(startDir)

	// convention over configuration
	pluginPath = filepath.Join(startDir, destFile)
	if _, err := os.Stat(pluginPath); err == nil {
		return pluginPath, nil
	}

	// system root dir
	parentDir, _ := filepath.Abs(filepath.Dir(startDir))
	if parentDir == startDir {
		if pluginPath, err = locateExecutable(destFile); err == nil {
			return
		}
		return "", errors.New("searched to system root dir, plugin file not found")
	}

	return locateFile(parentDir, destFile)
}

// locateExecutable finds destFile in hrp executable dir
func locateExecutable(destFile string) (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", errors.Wrap(err, "get hrp executable failed")
	}

	exeDir := filepath.Dir(exePath)
	pluginPath := filepath.Join(exeDir, destFile)
	if _, err := os.Stat(pluginPath); err == nil {
		return pluginPath, nil
	}

	return "", errors.New("plugin file not found in hrp executable dir")
}

func GetProjectRootDirPath(path string) (rootDir string, err error) {
	pluginPath, err := locatePlugin(path)
	if err == nil {
		rootDir = filepath.Dir(pluginPath)
		return
	}
	// fix: no debugtalk file in project but having proj.json created by startpeoject
	projPath, err := locateFile(path, projectInfoFile)
	if err == nil {
		rootDir = filepath.Dir(projPath)
		return
	}

	// failed to locate project root dir
	// maybe project plugin debugtalk.xx and proj.json are not exist
	// use current dir instead
	return env.RootDir, nil
}
