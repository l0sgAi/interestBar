package pgsql

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

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
	db.AutoMigrate(&model.SysUser{})

	DB = db
	if logger.Log != nil {
		logger.Log.Info("Database connection successful")
	}
}
