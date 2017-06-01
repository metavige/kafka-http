package main

import (
	"github.com/Shopify/sarama"

	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	addr    = flag.String("addr", ":8080", "The address to bind to")
	brokers = flag.String("brokers", os.Getenv("KAFKA_PEERS"), "The Kafka brokers to connect to, as a comma separated list")
	verbose = flag.Bool("verbose", false, "Turn on Sarama logging")
)

func main() {
	flag.Parse()

	if *verbose {
		sarama.Logger = log.New(os.Stdout, "[kafka-http] ", log.LstdFlags)
	}

	if *brokers == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	brokerList := strings.Split(*brokers, ",")
	log.Printf("Kafka brokers: %s", strings.Join(brokerList, ", "))

	server := &Server{
		DataCollector: newAsyncProducer(brokerList),
	}
	defer func() {
		if err := server.Close(); err != nil {
			log.Println("Failed to close server", err)
		}
	}()

	log.Fatal(server.Run(*addr))
}

type Server struct {
	DataCollector sarama.AsyncProducer
}

// 定義傳入的資料格式
type KafkaInfo struct {
	Topic string `json:topic`
	Value string `json:input_data`
}

func (s *Server) Close() error {
	if err := s.DataCollector.Close(); err != nil {
		log.Println("Failed to shut down data collector cleanly", err)
	}

	return nil
}

func (s *Server) Handler() http.Handler {
	return s.collectQueryStringData()
}

func (s *Server) Run(addr string) error {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	log.Printf("Listening for requests on %s...\n", addr)
	return httpServer.ListenAndServe()
}

func (s *Server) collectQueryStringData() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		// 把資料轉成物件
		var kafkaInfo KafkaInfo
		err := json.NewDecoder(r.Body).Decode(&kafkaInfo)
		// 解析發生錯誤
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// We are not setting a message key, which means that all messages will
		// be distributed randomly over the different partitions.

		// 同步傳遞訊息
		//partition, offset, err := s.DataCollector.SendMessage(&sarama.ProducerMessage{
		//	Topic: kafkaInfo.Topic,
		//	Value: sarama.StringEncoder(kafkaInfo.Value),
		//})
		//log.Printf("Message Partition: %d, Offset: %d\n", partition, offset)

		// 非同步傳遞訊息
		s.DataCollector.Input() <- &sarama.ProducerMessage{
			Topic: kafkaInfo.Topic,
			Value: sarama.StringEncoder(kafkaInfo.Value),
		}

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Failed to store your data:, %s", err)
		} else {
			// The tuple (topic, partition, offset) can be used as a unique identifier
			// for a message in a Kafka cluster.
			w.WriteHeader(http.StatusOK)
			//fmt.Fprintf(w, "Your data is stored with unique identifier important/%d/%d", partition, offset)
		}
	})
}

// 產生同步的 Producer
func newSyncProducer(brokerList []string) sarama.SyncProducer {

	// For the data collector, we are looking for strong consistency semantics.
	// Because we don't change the flush settings, sarama will try to produce messages
	// as fast as possible to keep latency low.

	// 同步的設定
	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll // Wait for all in-sync replicas to ack the message
	config.Producer.Retry.Max = 10                   // Retry up to 10 times to produce the message
	config.Producer.Return.Successes = true

	// On the broker side, you may want to change the following settings to get
	// stronger consistency guarantees:
	// - For your broker, set `unclean.leader.election.enable` to false
	// - For the topic, you could increase `min.insync.replicas`.

	producer, err := sarama.NewSyncProducer(brokerList, config)
	if err != nil {
		log.Fatalln("Failed to start Sarama producer:", err)
	}

	return producer
}

// 產生非同步的 Producer
func newAsyncProducer(brokerList []string) sarama.AsyncProducer {

	// For the data collector, we are looking for strong consistency semantics.
	// Because we don't change the flush settings, sarama will try to produce messages
	// as fast as possible to keep latency low.

	// 同步的設定
	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForLocal       // Only wait for the leader to ack
	config.Producer.Compression = sarama.CompressionSnappy   // Compress messages
	config.Producer.Flush.Frequency = 500 * time.Millisecond // Flush batches every 500ms

	// On the broker side, you may want to change the following settings to get
	// stronger consistency guarantees:
	// - For your broker, set `unclean.leader.election.enable` to false
	// - For the topic, you could increase `min.insync.replicas`.

	producer, err := sarama.NewAsyncProducer(brokerList, config)
	if err != nil {
		log.Fatalln("Failed to start Sarama producer:", err)
	}

	return producer
}
