package pgsql

import (
	"fmt"
	"interestBar/pkg/conf"
	commentdomain "interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// DBHolder 持有 *gorm.DB 的引用，供 composition 层依赖注入使用。
//
// 过渡期：当前它只是读取全局单例 DB，避免领域包直接 import 全局变量；
// 后续重构会把它改造为真正的持有者（在 composition 层构造连接后注入）。
type DBHolder struct{}

// Get 返回当前 *gorm.DB 实例（即全局 DB）。
func (h *DBHolder) Get() *gorm.DB { return DB }

// InitDB initializes the database connection using PostgreSQL.
func InitDB() {
	p := conf.Config.Pgsql
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s %s",
		p.Path, p.Username, p.Password, p.DbName, p.Port, p.Config)

	var logMode gormlogger.Interface
	if p.LogMode == "debug" {
		logMode = gormlogger.Default.LogMode(gormlogger.Info)
	} else {
		logMode = gormlogger.Default.LogMode(gormlogger.Error)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logMode,
	})
	if err != nil {
		if logger.Log != nil {
			logger.Log.Error("Failed to connect to database: " + err.Error())
		} else {
			fmt.Println("Failed to connect to database: " + err.Error())
		}
		os.Exit(1)
	}

	// Connection Pool Configuration
	sqlDB, _ := db.DB()
	sqlDB.SetMaxIdleConns(p.MaxIdleConns)
	sqlDB.SetMaxOpenConns(p.MaxOpenConns)

	// Auto Migrate
	// 注意：SysUser 仍在 pkg/server/model 中（auth OAuth provider 依赖它）。
	// Comment 已迁移到 comment 领域（pkg/domains/comment/domain）。
	// 其余领域（post/circle/user/like）由各自领域包自行管理迁移（或通过
	// 对应的 infrastructure 层调用），这里只保留启动时必须迁移的核心表。
	if err := db.AutoMigrate(&model.SysUser{}); err != nil {
		if logger.Log != nil {
			logger.Log.Error("Failed to auto-migrate SysUser: " + err.Error())
		} else {
			fmt.Println("Failed to auto-migrate SysUser: " + err.Error())
		}
		os.Exit(1)
	}
	if err := db.AutoMigrate(&commentdomain.Comment{}); err != nil {
		if logger.Log != nil {
			logger.Log.Error("Failed to auto-migrate Comment: " + err.Error())
		} else {
			fmt.Println("Failed to auto-migrate Comment: " + err.Error())
		}
		os.Exit(1)
	}

	DB = db
	if logger.Log != nil {
		logger.Log.Info("Database connection successful")
	}
}
