package conf

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var Config *AppConfig

type AppConfig struct {
	Server  Server  `mapstructure:"server" json:"server" yaml:"server"`
	App     App     `mapstructure:"app" json:"app" yaml:"app"`
	Log     Log     `mapstructure:"log" json:"log" yaml:"log"`
	Pgsql   Pgsql   `mapstructure:"pgsql" json:"pgsql" yaml:"pgsql"`
	Oauth   Oauth   `mapstructure:"oauth" json:"oauth" yaml:"oauth"`
	Redis   Redis   `mapstructure:"redis" json:"redis" yaml:"redis"`
	SaToken SaToken `mapstructure:"sa_token" json:"sa_token" yaml:"sa_token"`
}

type Server struct {
	Port int    `mapstructure:"port" json:"port" yaml:"port"`
	Mode string `mapstructure:"mode" json:"mode" yaml:"mode"`
}

type App struct {
	Name    string `mapstructure:"name" json:"name" yaml:"name"`
	Version string `mapstructure:"version" json:"version" yaml:"version"`
}

type Log struct {
	Level    string `mapstructure:"level" json:"level" yaml:"level"`
	Format   string `mapstructure:"format" json:"format" yaml:"format"`
	Director string `mapstructure:"director" json:"director" yaml:"director"`
}

// 新增：对应 yaml 中的 oauth 层级
type Oauth struct {
	Google Google `mapstructure:"google" json:"google" yaml:"google"`
}

// 新增：对应 yaml 中的 google 层级
type Google struct {
	ClientID     string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
}

type Pgsql struct {
	Path         string `mapstructure:"path" json:"path" yaml:"path"`
	Port         string `mapstructure:"port" json:"port" yaml:"port"`
	Config       string `mapstructure:"config" json:"config" yaml:"config"`
	DbName       string `mapstructure:"db_name" json:"db_name" yaml:"db_name"`
	Username     string `mapstructure:"username" json:"username" yaml:"username"`
	Password     string `mapstructure:"password" json:"password" yaml:"password"`
	MaxIdleConns int    `mapstructure:"max_idle_conns" json:"max_idle_conns" yaml:"max_idle_conns"`
	MaxOpenConns int    `mapstructure:"max_open_conns" json:"max_open_conns" yaml:"max_open_conns"`
	LogMode      string `mapstructure:"log_mode" json:"log_mode" yaml:"log_mode"`
}

type Redis struct {
	Host     string `mapstructure:"host" json:"host" yaml:"host"`
	Port     int    `mapstructure:"port" json:"port" yaml:"port"`
	Password string `mapstructure:"password" json:"password" yaml:"password"`
	D        int    `mapstructure:"db" json:"db" yaml:"db"`
	PoolSize int    `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size"`
}

type SaToken struct {
	TokenName     string `mapstructure:"token_name" json:"token_name" yaml:"token_name"`
	Timeout       int    `mapstructure:"timeout" json:"timeout" yaml:"timeout"`
	ActiveTimeout int    `mapstructure:"active_timeout" json:"active_timeout" yaml:"active_timeout"`
	IsConcurrent  bool   `mapstructure:"is_concurrent" json:"is_concurrent" yaml:"is_concurrent"`
	IsShare       bool   `mapstructure:"is_share" json:"is_share" yaml:"is_share"`
}

func InitConfig(path string) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		panic(fmt.Errorf("fatal error config file: %s", err))
	}

	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("config file changed:", e.Name)
		if err := v.Unmarshal(&Config); err != nil {
			fmt.Println(err)
		}
	})

	if err := v.Unmarshal(&Config); err != nil {
		panic(err)
	}
}
