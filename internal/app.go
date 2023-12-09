package main

import (
	"context"
	"fmt"
	"iot-poc/internal/infrastructure/configuration"
	"iot-poc/util/log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

const (
	maxConnectionRetryAttempts = 5
)

func main() {
	// initialize config
	config, err := configuration.InitializeConfig()
	if err != nil {
		log.Fatal(err)
	}

	// init db
	appConfig := config.GetConfig()
	dbConfig := appConfig.DB
	dbConnectionString := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbConfig.Host,
		dbConfig.Port,
		dbConfig.Username,
		dbConfig.Password,
		dbConfig.DBName,
	)
	_, err = initDB("postgres", dbConnectionString)
	if err != nil {
		log.Fatal(err)
	}

	mqttClient, err := initMQTT(appConfig.MQTT)
	if err != nil {
		log.Fatal(err)
	}
	server := initHTTPServer()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Shutdown Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Gracefully stop the server and its dependencies
	if err := server.Shutdown(ctx); err != nil {
		log.Error(err, "Server Shutdown Error")
	}

	// Closing mqtt connection
	mqttClient.Disconnect(1000)

	select {
	case <-ctx.Done():
		log.Info("timeout of 5 seconds.")
	default:
		log.Info("Server exiting")
	}
}

func initDB(driver, connectionString string) (*sqlx.DB, error) {
	var connectingError error
	for i := 0; i < maxConnectionRetryAttempts; i++ {
		log.Info(fmt.Sprintf("Connecting to DB (%d/%d)", i+1, maxConnectionRetryAttempts))
		db, err := sqlx.Connect("postgres", connectionString)
		if err != nil {
			connectingError = err
			time.Sleep(1 * time.Second)
			continue
		}

		log.Info("connected to DB")
		return db, nil
	}

	return nil, connectingError
}

func initHTTPServer() http.Server {
	r := gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowHeaders = []string{"Authorization", "Content-Type"}
	r.Use(cors.New(config))
	return http.Server{
		Addr:    ":5000",
		Handler: r,
	}
}

func initMQTT(config configuration.MQTTConfig) (mqtt.Client, error) {
	// Now we establish the connection to the mqtt broker
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.Broker)
	opts.SetClientID(config.ClientID)

	opts.SetOrderMatters(false)       // Allow out of order messages (use this option unless in order delivery is essential)
	opts.ConnectTimeout = time.Second // Minimal delays on connect
	opts.WriteTimeout = time.Second   // Minimal delays on writes
	opts.KeepAlive = 10               // Keepalive every 10 seconds so we quickly detect network outages
	opts.PingTimeout = time.Second    // local broker so response should be quick

	// Automate connection management (will keep trying to connect and will reconnect if network drops)
	opts.ConnectRetry = true
	opts.AutoReconnect = true

	//
	// Connect to the broker
	//
	client := mqtt.NewClient(opts)

	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	log.Info("connected to MQTT Broker")
	return client, nil
}
