// Copyright © 2021 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alibaba/sealer/utils/mount"

	"github.com/alibaba/sealer/common"

	"github.com/alibaba/sealer/logger"
	v1 "github.com/alibaba/sealer/types/api/v1"
	"github.com/alibaba/sealer/utils"
)

const (
	RegistryName       = "sealer-registry"
	RegistryBindDest   = "/var/lib/registry"
	RegistryMountUpper = "/var/lib/sealer/tmp/upper"
	RegistryMountWork  = "/var/lib/sealer/tmp/work"
)

type RegistryConfig struct {
	IP     string `yaml:"ip,omitempty"`
	Domain string `yaml:"domain,omitempty"`
	Port   string `yaml:"port,omitempty"`
}

func getRegistryHost(rootfs, defaultRegistry string) (host string) {
	cf := GetRegistryConfig(rootfs, defaultRegistry)
	return fmt.Sprintf("%s %s", cf.IP, cf.Domain)
}

func GetRegistryConfig(rootfs, defaultRegistry string) *RegistryConfig {
	var config RegistryConfig
	var DefaultConfig = &RegistryConfig{
		IP:     utils.GetHostIP(defaultRegistry),
		Domain: SeaHub,
		Port:   "5000",
	}
	registryConfigPath := filepath.Join(rootfs, "/etc/registry.yaml")
	if !utils.IsFileExist(registryConfigPath) {
		logger.Debug("use default registry config")
		return DefaultConfig
	}
	err := utils.UnmarshalYamlFile(registryConfigPath, &config)
	logger.Info(fmt.Sprintf("show registry info, IP: %s, Domain: %s", config.IP, config.Domain))
	if err != nil {
		logger.Error("Failed to read registry config! ")
		return DefaultConfig
	}
	if config.IP == "" {
		config.IP = DefaultConfig.IP
	} else {
		config.IP = utils.GetHostIP(config.IP)
	}
	if config.Port == "" {
		config.Port = DefaultConfig.Port
	}
	if config.Domain == "" {
		config.Domain = DefaultConfig.Domain
	}
	return &config
}

//Only use this for join and init, due to the initiation operations
func (d *Default) EnsureRegistry(cluster *v1.Cluster) error {
	var (
		lowerLayers []string
		target      = fmt.Sprintf("%s/registry", d.Rootfs)
	)
	lowerLayers = append(lowerLayers, target)
	//get docker image layer
	im, err := d.imageStore.GetByName(cluster.Spec.Image)
	if err != nil {
		return err
	}

	layerDirs := getDockerImageDiffLayerDir(im)
	if len(layerDirs) != 0 {
		lowerLayers = append(lowerLayers, layerDirs...)
	}
	// todo need to revers low layers
	cf := GetRegistryConfig(d.Rootfs, d.Masters[0])
	mkdir := fmt.Sprintf("rm -rf %s %s && mkdir -p %s %s", RegistryMountUpper, RegistryMountWork,
		RegistryMountUpper, RegistryMountWork)

	mountCmd := fmt.Sprintf("%s && mount -t overlay overlay -o lowerdir=%s,upperdir=%s,workdir=%s %s", mkdir,
		strings.Join(utils.Reverse(lowerLayers), ":"),
		RegistryMountUpper, RegistryMountWork, target)

	if err := d.SSH.CmdAsync(cf.IP, mountCmd); err != nil {
		return err
	}

	cmd := fmt.Sprintf("cd %s/scripts && sh init-registry.sh %s %s", d.Rootfs, cf.Port, target)
	return d.SSH.CmdAsync(cf.IP, cmd)
}

func (d *Default) RecycleRegistry() error {
	cf := GetRegistryConfig(d.Rootfs, d.Masters[0])
	umount := fmt.Sprintf("umount %s/registry", d.Rootfs)
	isMount, _ := mount.GetRemoteMountDetails(d.SSH, cf.IP, filepath.Join(d.Rootfs, "registry"))
	if isMount {
		err := d.SSH.CmdAsync(cf.IP, umount)
		if err != nil {
			return fmt.Errorf("failed to %s in %s, %v", umount, cf.IP, err)
		}
	}
	delDir := fmt.Sprintf("rm -rf %s %s", RegistryMountUpper, RegistryMountWork)
	cmd := fmt.Sprintf("docker rm -f %s && %s ", RegistryName, delDir)
	return d.SSH.CmdAsync(cf.IP, cmd)
}

//get docker image layer hash path
func getDockerImageDiffLayerDir(image *v1.Image) (res []string) {
	for _, layer := range image.Spec.Layers {
		if layer.ID != "" && layer.Type == common.BaseImageLayerType {
			res = append(res, filepath.Join(common.DefaultLayerDir, layer.ID.Hex()))
		}
	}
	return
}
