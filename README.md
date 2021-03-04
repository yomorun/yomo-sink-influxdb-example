# yomo-sink-influxdb-example

InfluxDB 🙌 YoMo. Demonstrates how to integrate InfluxDB to YoMo and bulk insert data into InfluxDB after stream processing.

## About InfluxDB

InfluxDB is an open source time series platform. This includes APIs for storing and querying data, processing it in the background for ETL or monitoring and alerting purposes, user dashboards, and visualizing and exploring the data and more.

For more information, please visit [InfluxDB homepage](https://www.influxdata.com/products/influxdb/).

## About YoMo

[YoMo](https://github.com/yomorun/yomo) is an open-source Streaming Serverless Framework for building Low-latency Edge Computing applications. Built atop QUIC Transport Protocol and Functional Reactive Programming interface. makes real-time data processing reliable, secure, and easy.

## 1: Download and install InfluxDB v2.0

### Run InfluxDB v2.0 via Docker

```bash
docker run -d -p 8086:8086 quay.io/influxdb/influxdb:v2.0.4
```

See [Get started with InfluxDB](https://docs.influxdata.com/influxdb/v2.0/get-started/) for more information.

## 2: Set up InfluxDB

### Set up InfluxDB through the UI

1. With InfluxDB running, visit [localhost:8086](http://localhost:8086/).
2. Click Get Started

### Set up your initial user

1. Enter a Username for your initial user.
2. Enter a Password and Confirm Password for your user.
3. Enter your initial Organization Name, f.e. `yomo`.
4. Enter your initial Bucket Name, f.e. `noise`.
5. Click Continue.

## 3: Integrate InfluxDB with YoMo

### Start YoMo-Zipper

Configure [YoMo-Zipper](https://yomo.run/zipper):

```yaml
name: YoMoZipper 
host: localhost
port: 9000
sinks:
  - name: InfluxDB
    host: localhost
    port: 9321
```

Start this zipper will listen on `9000` port, send data streams directly to `9321`:

```bash
cd ./zipper && yomo wf run
```

### Store data to InfluxDB

```go
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
		BufferWithCount(batchSize) // buffer 100 noises in batch
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

```

Start this [YoMo-Sink](https://yomo.run/sink), will save data to InfluxDB wherever data arrives.

```bash
INFLUXDB_TOKEN=your-influxdb-token go run main.go
```

| env             | **description**|
| --------------- | ----------------------------------------------------------------------------------|
| INFLUXDB_TOKEN  | **Required:** Your InfuxDB Auth Token. You can generate a Token from the "Tokens Tab" in the InfluxDB UI. |
| INFLUXDB_URL    | The URL of your InfluxDB Server. Default: http://localhost:8086 |
| INFLUXDB_ORG    | Your Organization Name. Default: yomo |
| INFLUXDB_BUCKET | Your Bucket Name. Default: noise |

### Emulate a data source for testing

```bash
cd source && go run main.go
```

This will start a [YoMo-Source](https://yomo.run/source), demonstrate a random float every 100ms to [YoMo-Zipper](https://yomo.run/zipper).

## 4. Verify data in InfluxDB

### Query InfluxDB with Flux

```bash
from(bucket: "noise")
  |> range(start: -15m)

          _time            |         _value        |
====================================================
 2021-03-04 16:36:24 GMT+8 |             69.061432 |
 2021-03-04 16:36:24 GMT+8 |            140.104614 |
 2021-03-04 16:36:24 GMT+8 |            130.500717 |
 2021-03-04 16:36:24 GMT+8 |             157.36317 |
 2021-03-04 16:36:24 GMT+8 |              9.002395 |
 2021-03-04 16:36:24 GMT+8 |            167.233383 |
 2021-03-04 16:36:24 GMT+8 |             89.616028 |
 2021-03-04 16:36:24 GMT+8 |            105.419456 |
 2021-03-04 16:36:24 GMT+8 |             28.253075 |
 2021-03-04 16:36:24 GMT+8 |            146.267715 |
 2021-03-04 16:36:24 GMT+8 |             93.725861 |
 2021-03-04 16:36:24 GMT+8 |             50.364281 |
```
