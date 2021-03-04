package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxapi2 "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/reactivex/rxgo/v2"
	y3 "github.com/yomorun/y3-codec-golang"
	"github.com/yomorun/yomo/pkg/quic"
)

const batchSize = 1000

var bufferTime = rxgo.WithDuration(30 * time.Second)

var (
	client         influxdb2.Client
	writeAPI       influxapi2.WriteAPI
	serverURL      = os.Getenv("INFLUXDB_URL")    // default: http://localhost:8086
	org            = os.Getenv("INFLUXDB_ORG")    // default: yomo
	bucket         = os.Getenv("INFLUXDB_BUCKET") // default: noise
	token          = os.Getenv("INFLUXDB_TOKEN")  // you can generate a Token from the "Tokens Tab" in the UI
	sinkServerAddr = "0.0.0.0:9321"
)

func init() {
	if token == "" {
		panic("please set the token in env INFLUXDB_TOKEN")
	}
	if serverURL == "" {
		serverURL = "http://localhost:8086"
	}
	if org == "" {
		org = "yomo"
	}
	if bucket == "" {
		bucket = "noise"
	}

	// create a new InfluxDB Client
	client = influxdb2.NewClient(serverURL, token)
	influxdb2.DefaultOptions().SetBatchSize(batchSize)
	writeAPI = client.WriteAPI(org, bucket)
	// Get errors channel
	errorsCh := writeAPI.Errors()
	// Create go proc for reading and logging errors
	go func() {
		for err := range errorsCh {
			log.Printf("write error: %s\n", err.Error())
		}
	}()
}

func main() {
	// always close client at the end
	defer client.Close()

	log.Print("Starting YoMo Sink server: -> InfluxDB")
	srv := quic.NewServer(&srvHandler{})
	err := srv.ListenAndServe(context.Background(), sinkServerAddr)
	if err != nil {
		log.Printf("YoMo Sink server start failed: %s\n", err.Error())
	}
}

type srvHandler struct {
}

func (s *srvHandler) Listen() error {
	return nil
}

func (s *srvHandler) Read(qs quic.Stream) error {
	ch := y3.FromStream(qs).
		Subscribe(0x10).
		OnObserve(decode)

	insertNoisesInBatch(ch)

	return nil
}

// decode the noise value via Y3
func decode(v []byte) (interface{}, error) {
	data, err := y3.ToFloat32(v)
	if err != nil {
		log.Printf("err: %s\n", err.Error())
	}
	return data, err
}

// insert noises into InfluxDB in batch
func insertNoisesInBatch(ch chan interface{}) {
	next := make(chan rxgo.Item)

	go func() {
		defer close(next)
		for item := range ch {
			next <- rxgo.Of(item)
		}
	}()

	observable := rxgo.FromChannel(next).
		BufferWithTimeOrCount(bufferTime, batchSize) // buffer 30s or 1000 noises in batch
	go bulkInsert(observable)
}

func bulkInsert(observable rxgo.Observable) {
	for ch := range observable.Observe() {
		if ch.Error() {
			log.Println(ch.E.Error())
		} else if ch.V != nil {
			// bulk insert
			items := ch.V.([]interface{})
			for _, item := range items {
				line := fmt.Sprintf("noise_sensor val=%f %s", item, strconv.FormatInt(time.Now().UnixNano(), 10))
				writeAPI.WriteRecord(line)
			}
			// save the points in batch
			writeAPI.Flush()
			log.Printf("Insert %d noise values into InfluxDB...", len(items))
		}
	}
}
