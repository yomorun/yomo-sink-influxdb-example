package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxapi2 "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/reactivex/rxgo/v2"
	y3 "github.com/yomorun/y3-codec-golang"
	"github.com/yomorun/yomo/pkg/quic"
	"github.com/yomorun/yomo/pkg/rx"
)

const batchSize = 1000
const bufferSeconds = 30

var bufferTime = rxgo.WithDuration(bufferSeconds * time.Second)

var (
	client         influxdb2.Client
	writeAPI       influxapi2.WriteAPI
	serverURL      = os.Getenv("INFLUXDB_URL")    // default: http://localhost:8086
	org            = os.Getenv("INFLUXDB_ORG")    // default: yomo
	bucket         = os.Getenv("INFLUXDB_BUCKET") // default: thermometer
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
		bucket = "thermometer"
	}

	// Create a new InfluxDB Client
	client = influxdb2.NewClient(serverURL, token)
	writeAPI = client.WriteAPI(org, bucket)
	// Set options
	influxdb2.DefaultOptions().SetBatchSize(batchSize)
	influxdb2.DefaultOptions().SetFlushInterval(bufferSeconds * 1000)
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
	srv := quic.NewServer(&srvHandler{
		readers: make(chan io.Reader),
	})
	err := srv.ListenAndServe(context.Background(), sinkServerAddr)
	if err != nil {
		log.Printf("YoMo Sink server start failed: %s\n", err.Error())
	}
}

type srvHandler struct {
	readers chan io.Reader
}

func (s *srvHandler) Listen() error {
	rxstream := rx.FromReaderWithY3(s.readers)
	observer := rxstream.Subscribe(0x10).
		OnObserve(decode).
		BufferWithTimeOrCount(bufferTime, batchSize)

	rxstream.Connect(context.Background())

	go bulkInsert(observer)
	return nil
}

func (s *srvHandler) Read(qs quic.Stream) error {
	s.readers <- qs
	return nil
}

type ThermometerData struct {
	Temperature float32 `y3:"0x11" json:"tem"`
	Humidity    float32 `y3:"0x12" json:"hum"`
}

// decode the thermometer value via Y3
func decode(v []byte) (interface{}, error) {
	var data ThermometerData
	err := y3.ToObject(v, &data)
	if err != nil {
		log.Printf("err: %s\n", err.Error())
	}
	return data, err
}

// bulk insert the data into InfluxDB
func bulkInsert(observer rx.RxStream) {
	for ch := range observer.Observe() {
		if ch.Error() {
			log.Println(ch.E.Error())
		} else if ch.V != nil {
			// bulk insert
			items, ok := ch.V.([]ThermometerData)
			if !ok {
				log.Println(ok)
				continue
			}

			for _, item := range items {
				line := fmt.Sprintf("thermometer_sensor tem=%f,hum=%f %s", item.Temperature, item.Humidity, strconv.FormatInt(time.Now().UnixNano(), 10))
				// WriteRecord adds record into the buffer which is sent on the background when it reaches the batch size.
				writeAPI.WriteRecord(line)
			}
			// Flush forces all pending writes from the buffer to be sent
			writeAPI.Flush()
			log.Printf("Insert %d thermometer values into InfluxDB...", len(items))
		}
	}
}
