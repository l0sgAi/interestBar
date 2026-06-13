package main

import (
	"flag"
	"interestBar/cmd/apps"
)

func main() {
	var config, bootstrap string
	flag.StringVar(&config, "c", "configs/config.yaml", "本地兜底配置文件(Nacos 不可用时使用)。")
	flag.StringVar(&bootstrap, "b", "configs/bootstrap.yaml", "Nacos 引导文件(含服务端地址/鉴权/环境映射)。不存在或为空时跳过 Nacos，直接加载 -c。")
	flag.Parse()

	apps.Run(config, bootstrap)
}
