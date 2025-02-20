// Copyright © 2021 github.com/wonderivan/logger
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

package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/alibaba/sealer/common"
)

type connLogger struct {
	sync.Mutex
	innerWriter    io.WriteCloser
	ReconnectOnMsg bool   `json:"reconnectOnMsg"`
	Reconnect      bool   `json:"reconnect"`
	Net            string `json:"net"`
	Addr           string `json:"addr"`
	Level          string `json:"level"`
	LogLevel       logLevel
	illNetFlag     bool //网络异常标记
}

func (c *connLogger) Init(jsonConfig string) error {
	if len(jsonConfig) == 0 {
		return nil
	}
	fmt.Printf("consoleWriter Init:%s\n", jsonConfig)
	err := json.Unmarshal([]byte(jsonConfig), c)
	if err != nil {
		return err
	}
	if l, ok := LevelMap[c.Level]; ok {
		c.LogLevel = l
	}
	if c.innerWriter != nil {
		c.innerWriter.Close()
		c.innerWriter = nil
	}
	return nil
}

func (c *connLogger) LogWrite(when time.Time, msgText interface{}, level logLevel) (err error) {
	if level > c.LogLevel {
		return nil
	}

	msg, ok := msgText.(*loginfo)
	if !ok {
		return
	}

	if c.needToConnectOnMsg() {
		err = c.connect()
		if err != nil {
			return
		}
		//重连成功
		c.illNetFlag = false
	}

	//每条消息都重连一次日志中心，适用于写日志频率极低的情况下的服务调用,避免长时间连接，占用资源
	if c.ReconnectOnMsg { // 频繁日志发送切勿开启
		defer c.innerWriter.Close()
	}

	//网络异常时，消息发出
	if !c.illNetFlag {
		err = c.println(when, msg)
		//网络异常，通知处理网络的go程自动重连
		if err != nil {
			c.illNetFlag = true
		}
	}

	return
}

func (c *connLogger) Destroy() {
	if c.innerWriter != nil {
		c.innerWriter.Close()
	}
}

func (c *connLogger) connect() error {
	if c.innerWriter != nil {
		c.innerWriter.Close()
		c.innerWriter = nil
	}
	addrs := strings.Split(c.Addr, ";")
	for _, addr := range addrs {
		conn, err := net.Dial(c.Net, addr)
		if err != nil {
			fmt.Fprintf(common.StdErr, "net.Dial error:%v\n", err)
			continue
			//return err
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			err = tcpConn.SetKeepAlive(true)
			if err != nil {
				fmt.Fprintf(common.StdErr, "failed to set tcp keep alive :%v\n", err)
				continue
			}
		}
		c.innerWriter = conn
		return nil
	}
	return fmt.Errorf("hava no valid logs service addr:%v", c.Addr)
}

func (c *connLogger) needToConnectOnMsg() bool {
	if c.Reconnect {
		c.Reconnect = false
		return true
	}

	if c.innerWriter == nil {
		return true
	}

	if c.illNetFlag {
		return true
	}
	return c.ReconnectOnMsg
}

func (c *connLogger) println(when time.Time, msg *loginfo) error {
	c.Lock()
	defer c.Unlock()
	ss, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.innerWriter.Write(append(ss, '\n'))

	//返回err，解决日志系统网络异常后的自动重连
	return err
}

func init() {
	Register(AdapterConn, &connLogger{LogLevel: LevelTrace})
}
