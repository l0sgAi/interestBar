package conf

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var Config *AppConfig

type AppConfig struct {
	Server       Server       `mapstructure:"server" json:"server" yaml:"server"`
	CORS         CORS         `mapstructure:"cors" json:"cors" yaml:"cors"`
	App          App          `mapstructure:"app" json:"app" yaml:"app"`
	Log          Log          `mapstructure:"log" json:"log" yaml:"log"`
	Pgsql        Pgsql        `mapstructure:"pgsql" json:"pgsql" yaml:"pgsql"`
	Oauth        Oauth        `mapstructure:"oauth" json:"oauth" yaml:"oauth"`
	Redis        Redis        `mapstructure:"redis" json:"redis" yaml:"redis"`
	SaToken      SaToken      `mapstructure:"sa_token" json:"sa_token" yaml:"sa_token"`
	S3           S3           `mapstructure:"s3" json:"s3" yaml:"s3"`
	Elasticsearch Elasticsearch `mapstructure:"elasticsearch" json:"elasticsearch" yaml:"elasticsearch"`
	RabbitMQ     RabbitMQ     `mapstructure:"rabbitmq" json:"rabbitmq" yaml:"rabbitmq"`
}

type Server struct {
	Port int    `mapstructure:"port" json:"port" yaml:"port"`
	Mode string `mapstructure:"mode" json:"mode" yaml:"mode"`
}

type CORS struct {
	AllowedOrigins []string `mapstructure:"allowed_origins" json:"allowed_origins" yaml:"allowed_origins"`
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
	Github Github `mapstructure:"github" json:"github" yaml:"github"`
}

// 新增:对应 yaml 中的 google 层级
type Google struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
}

// 新增:对应 yaml 中的 github 层级
type Github struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
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

// S3 AWS S3 对象存储配置
type S3 struct {
	AccessKeyID       string `mapstructure:"access_key_id" json:"access_key_id" yaml:"access_key_id"`
	SecretAccessKey   string `mapstructure:"secret_access_key" json:"secret_access_key" yaml:"secret_access_key"`
	Region            string `mapstructure:"region" json:"region" yaml:"region"`
	Bucket            string `mapstructure:"bucket" json:"bucket" yaml:"bucket"`
	Endpoint          string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`
	PresignURLExpire  int    `mapstructure:"presign_url_expire" json:"presign_url_expire" yaml:"presign_url_expire"`
	CloudfrontDomain  string `mapstructure:"cloudfront_domain" json:"cloudfront_domain" yaml:"cloudfront_domain"`
}

// Elasticsearch Elasticsearch 配置
type Elasticsearch struct {
	URL             string `mapstructure:"url" json:"url" yaml:"url"`
	Index           string `mapstructure:"index" json:"index" yaml:"index"`
	RefreshInterval string `mapstructure:"refresh_interval" json:"refresh_interval" yaml:"refresh_interval"`
}

// RabbitMQ RabbitMQ 配置
type RabbitMQ struct {
	Host       string      `mapstructure:"host" json:"host" yaml:"host"`
	Port       int         `mapstructure:"port" json:"port" yaml:"port"`
	Username   string      `mapstructure:"username" json:"username" yaml:"username"`
	Password   string      `mapstructure:"password" json:"password" yaml:"password"`
	VHost      string      `mapstructure:"vhost" json:"vhost" yaml:"vhost"`
	Exchange   string      `mapstructure:"exchange" json:"exchange" yaml:"exchange"`
	Queue      string      `mapstructure:"queue" json:"queue" yaml:"queue"`
	RoutingKey string      `mapstructure:"routing_key" json:"routing_key" yaml:"routing_key"`
	Retry      RabbitMQRetry `mapstructure:"retry" json:"retry" yaml:"retry"`
}

// RabbitMQRetry RabbitMQ 重试配置
type RabbitMQRetry struct {
	MaxAttempts int `mapstructure:"max_attempts" json:"max_attempts" yaml:"max_attempts"`
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
