// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldkafka

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Shopify/sarama"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type testKafkaGoRoutines struct{}

func (g *testKafkaGoRoutines) ConsumerMessagesLoop(consumer KafkaConsumer, producer KafkaProducer, wg *sync.WaitGroup) {
	for msg := range consumer.Messages() {
		log.Infof("msg: %+v", msg)
	}
	wg.Done()
}

func (g *testKafkaGoRoutines) ProducerErrorLoop(consumer KafkaConsumer, producer KafkaProducer, wg *sync.WaitGroup) {
	for err := range producer.Errors() {
		log.Infof("err: %s", err)
	}
	wg.Done()
}

func (g *testKafkaGoRoutines) ProducerSuccessLoop(consumer KafkaConsumer, producer KafkaProducer, wg *sync.WaitGroup) {
	for msg := range producer.Successes() {
		log.Infof("msg: %+v", msg)
	}
	wg.Done()
}

var kcMinWorkingArgs = []string{
	"-b", "broker1",
	"-t", "in-topic",
	"-T", "out-topic",
	"-g", "test-group",
}

// newTestKafkaCommon creates a KafkaCommon instance with a Cobra command wrapper
func newTestKafkaCommon(testArgs []string) (*kafkaCommon, *cobra.Command) {
	log.SetLevel(log.DebugLevel)
	gr := &testKafkaGoRoutines{}
	k := NewKafkaCommon(NewMockKafkaFactory(), &KafkaCommonConf{}, gr).(*kafkaCommon)

	kafkaCmd := &cobra.Command{
		RunE: func(cmd *cobra.Command, args []string) error {
			return k.Start()
		},
		PreRunE: func(cmd *cobra.Command, args []string) (err error) {
			err = k.ValidateConf()
			return
		},
	}
	k.CobraInit(kafkaCmd)
	kafkaCmd.SetArgs(testArgs)
	return k, kafkaCmd
}

// startTestKafkaCommon starts a KafkaCommon instance with the specified args
func startTestKafkaCommon(assert *assert.Assertions, testArgs []string, f *MockKafkaFactory) (k *kafkaCommon, wg *sync.WaitGroup, err error) {
	k, kafkaCmd := newTestKafkaCommon(testArgs)
	k.factory = f
	wg = &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err = kafkaCmd.Execute()
		wg.Done()
	}()
	for k.signals == nil && err == nil {
		time.Sleep(10 * time.Millisecond)
	}
	return k, wg, err
}

// execKafkaCommonWithArgs starts, then terminates, a KafkaCommon instance
func execKafkaCommonWithArgs(assert *assert.Assertions, testArgs []string, f *MockKafkaFactory) (k *kafkaCommon, err error) {
	k, wg, err := startTestKafkaCommon(assert, testArgs, f)
	if err == nil {
		k.signals <- os.Interrupt
	}
	wg.Wait()
	return
}

func TestNewKafkaCommon(t *testing.T) {
	assert := assert.New(t)
	gr := &testKafkaGoRoutines{}
	k := NewKafkaCommon(NewMockKafkaFactory(), &KafkaCommonConf{}, gr)
	assert.NotNil(k.Conf())
}

func TestExecuteWithIncompleteArgs(t *testing.T) {
	assert := assert.New(t)
	f := NewMockKafkaFactory()

	testArgs := []string{}
	_, err := execKafkaCommonWithArgs(assert, testArgs, f)
	assert.Equal("No output topic specified for bridge to send events to", err.Error())
	testArgs = append(testArgs, []string{"-T", "test"}...)

	_, err = execKafkaCommonWithArgs(assert, testArgs, f)
	assert.Equal("No input topic specified for bridge to listen to", err.Error())
	testArgs = append(testArgs, []string{"-t", "test-in"}...)

	_, err = execKafkaCommonWithArgs(assert, testArgs, f)
	assert.Equal("No consumer group specified", err.Error())
	testArgs = append(testArgs, []string{"-g", "test-group"}...)

	testArgs = append(testArgs, []string{"-b", "broker1", "--tls-clientcerts", "/some/file"}...)
	_, err = execKafkaCommonWithArgs(assert, testArgs, f)
	assert.Equal("Client private key and certificate must both be provided for mutual auth", err.Error())
	testArgs = append(testArgs, []string{"--tls-clientkey", "somekey"}...)

	testArgs = append(testArgs, []string{"--sasl-username", "testuser"}...)
	_, err = execKafkaCommonWithArgs(assert, testArgs, f)
	assert.Equal("Username and Password must both be provided for SASL", err.Error())
	testArgs = append(testArgs, []string{"--sasl-password", "testpass"}...)

}

func TestExecuteWithNewClientError(t *testing.T) {
	assert := assert.New(t)

	_, err := execKafkaCommonWithArgs(assert, kcMinWorkingArgs, NewErrorMockKafkaFactory(
		fmt.Errorf("pop"), nil, nil,
	))

	assert.Regexp("pop", err.Error())

}

func TestExecuteWithNewProducerError(t *testing.T) {
	assert := assert.New(t)

	_, err := execKafkaCommonWithArgs(assert, kcMinWorkingArgs, NewErrorMockKafkaFactory(
		nil, fmt.Errorf("fizz"), nil,
	))

	assert.Regexp("fizz", err.Error())

}
func TestExecuteWithNewConsumerError(t *testing.T) {
	assert := assert.New(t)

	_, err := execKafkaCommonWithArgs(assert, kcMinWorkingArgs, NewErrorMockKafkaFactory(
		nil, nil, fmt.Errorf("bang"),
	))

	assert.Regexp("bang", err.Error())

}
func TestExecuteWithNoTLS(t *testing.T) {
	assert := assert.New(t)

	f := NewMockKafkaFactory()
	k, err := execKafkaCommonWithArgs(assert, kcMinWorkingArgs, f)
	assert.NotNil(k.Producer())

	assert.NoError(err)
	assert.Equal(true, f.ClientConf.Producer.Return.Successes)
	assert.Equal(true, f.ClientConf.Producer.Return.Errors)
	assert.Equal(sarama.WaitForLocal, f.ClientConf.Producer.RequiredAcks)
	assert.Equal(time.Duration(0), f.ClientConf.Producer.Flush.Frequency)
	assert.Equal(true, f.ClientConf.Consumer.Return.Errors)
	assert.Equal(false, f.ClientConf.Net.TLS.Enable)
	assert.Equal((*tls.Config)(nil), f.ClientConf.Net.TLS.Config)
	assert.Regexp("\\w+", f.ClientConf.ClientID) // generated UUID

}

func TestExecuteWithSASL(t *testing.T) {
	assert := assert.New(t)

	f := NewMockKafkaFactory()
	_, err := execKafkaCommonWithArgs(assert, []string{
		"-b", "broker1",
		"-t", "in-topic",
		"-T", "out-topic",
		"-g", "test-group",
		"-u", "testuser",
		"-p", "testpass",
	}, f)

	assert.Equal(nil, err)
	assert.Equal("testuser", f.ClientConf.Net.SASL.User)
	assert.Equal("testpass", f.ClientConf.Net.SASL.Password)

}

func TestExecuteWithDefaultTLSAndClientID(t *testing.T) {
	assert := assert.New(t)

	f := NewMockKafkaFactory()
	_, err := execKafkaCommonWithArgs(assert, []string{
		"-b", "broker1",
		"-t", "in-topic",
		"-T", "out-topic",
		"-g", "test-group",
		"-i", "clientid1",
		"--tls-enabled",
	}, f)

	assert.Equal(nil, err)
	assert.Equal(true, f.ClientConf.Net.TLS.Enable)
	assert.Equal(0, len(f.ClientConf.Net.TLS.Config.Certificates))
	assert.Equal((*x509.CertPool)(nil), f.ClientConf.Net.TLS.Config.RootCAs)
	assert.Equal(false, f.ClientConf.Net.TLS.Config.InsecureSkipVerify)
	assert.Equal("clientid1", f.ClientConf.ClientID) // generated UUID

}

func TestMissingBroker(t *testing.T) {
	assert := assert.New(t)

	f := NewMockKafkaFactory()
	_, err := execKafkaCommonWithArgs(assert, []string{
		"-t", "in-topic",
		"-T", "out-topic",
		"-g", "test-group",
		"-i", "clientid1",
	}, f)

	assert.EqualError(err, "No Kafka brokers configured")

}

func TestExecuteWithSelfSignedMutualAuth(t *testing.T) {
	assert := assert.New(t)

	testPrivKeyFile, _ := ioutil.TempFile("", "testprivkey")
	defer syscall.Unlink(testPrivKeyFile.Name())
	ioutil.WriteFile(testPrivKeyFile.Name(), []byte(
		"-----BEGIN PRIVATE KEY-----\n"+
			"MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDXCA6IdwIgFeRw\n"+
			"MSEJSbKWtujq4+8DUEZIYRLbYXq2oblrK6ObmtkdBOUktDGeVbeaffEHJsqN5pYE\n"+
			"n9WBMdxxYkQtov5DX8Gbmx8IUr74NtosCG2jWW5yVjdMYhf8IGPUQsV6Za5L2VBX\n"+
			"o4kd6wA25AKYz+xao0WFxHiqDTJteWGvl+e44IinrQ2yTfcB43PjYUcqav4VO1dt\n"+
			"VrDSMcVs6vtXLzX4sFewqudrACirJOgtZs5kfLYQO5coTN35mUIeY8eRUtWapq8e\n"+
			"bhtTbO4gxOyfwJ3Ck2A3mk6TcZYLT/g+6X8FxohGzymbBYTr7HXb6f/GC1DQu0hg\n"+
			"SP98+y1pAgMBAAECggEAQY1wOMPm/vcNk/I2Owmfivip2umvtJflRS1qvTxjV4fH\n"+
			"6db84nP7WjBi1qSkN7uz5EIel2qI92djNnevc9pKdLpbRHpa/xkTAafxdu0a0LqQ\n"+
			"Gjpbih+6XtrPstZ4r2EEbfIJF74lu3O9XWo6Y8d/YjxyWjmQuTTq/dOeYWDyjZKT\n"+
			"WgpaneD2vxRAwvUCaypvGNs20FjX7MhgB7Xj2MDg3t04C10s/G7IGyncuZfrnZU9\n"+
			"LA70aZEaW/TkbWyzEMr4ObjjkIO0Xlr1pp1fNIGIFc39tzQoKDhLP/0t4MFXrgm8\n"+
			"/ff+sW7pNd2sbZHaUPTzBYEa/D4XcLVqt/479F4SHQKBgQDsEzVBFyWCBJ2UiCYw\n"+
			"jmMS8jURi9k5DuwSUOPGkJgRI0U6uyXh6s8u9UHPbgKhMf9IUs2fMaGG338tDRyP\n"+
			"SDT5x84jCoIKjj9CXKHbsfmHW6EzHJqXgYX3rnv19cwpJ2a1Wryoljca+zecwpUX\n"+
			"CNXTm3oRGj6aim8VYRcyaQfEdwKBgQDpLir0iNoq3CuGJFqTYydFQuSWIs8DszXm\n"+
			"12k4kFe0+6ANM2yxM0JkvPljLz/Pfb+1mDKK5dqBlILZ/T3100hpvzEueJoeoT2q\n"+
			"a7rF9+6iOF62DNfyis0DxFmB8KYf4GUaWnGI7z0UVZaFdjhvl/xATMMcaSFZVA+m\n"+
			"cNm42cB1HwKBgDGwMUtL9ecR1aEHrxIVRiEcvbK9vrDVxTZttCN9F6SzycR805Jj\n"+
			"e8wkbv+b5g3LmjG8y+6v4ZGjxP7UfahiyFOyjF6vvYM/QW1UVfUJ1r14ucsqQBeX\n"+
			"eX0SSqEQZTJcSq/tMzxAscSKD8B87Ch3AZqSZPTokziv3oWfc+R2Wt4tAoGBAILa\n"+
			"Np69QXi1zvLa6b018i6q6C3cYMFZyxC8pz5nueBFKD7gMcmK02JGrchcFnnwvilA\n"+
			"vHQ3opP+7CM6Oo/9vfAhq47BfPNdVoaRJ+G6TT7ZVUTiFjj0bTIE+JmzmvXebb4J\n"+
			"LRdD8cm8cdh5TBhLePH4YbFKyb0gMBwdzgAuqhLPAoGBAMuMa5gEGNvUMRA5hymp\n"+
			"IKMoO01vmc7c8gJmunFujMZS1fQQA2Qzw16z6ekZgLVgLz27ivfh203qgqORWyXg\n"+
			"HvrK9nTzXLEnYprg0bmyivjo0LAk0ISuAdTSWGWhUZqLON8g5T9AmCGmtFE6FZEl\n"+
			"YV406bLxh5diFcQIEFGfgWNe\n"+
			"-----END PRIVATE KEY-----\n"), 0644)

	testCertData := []byte(
		"-----BEGIN CERTIFICATE-----\n" +
			"MIIDYjCCAkoCCQCl+tdkvcUkzTANBgkqhkiG9w0BAQsFADBzMQswCQYDVQQGEwJV\n" +
			"UzELMAkGA1UECAwCTkMxEDAOBgNVBAcMB1JhbGVpZ2gxEDAOBgNVBAoMB0thbGVp\n" +
			"ZG8xFTATBgNVBAsMDFVuaXQgdGVzdGluZzEcMBoGA1UEAwwTdW5pdHRlc3RAa2Fs\n" +
			"ZWlkby5pbzAeFw0xODA2MjUxOTAzMzJaFw0xODA3MjUxOTAzMzJaMHMxCzAJBgNV\n" +
			"BAYTAlVTMQswCQYDVQQIDAJOQzEQMA4GA1UEBwwHUmFsZWlnaDEQMA4GA1UECgwH\n" +
			"S2FsZWlkbzEVMBMGA1UECwwMVW5pdCB0ZXN0aW5nMRwwGgYDVQQDDBN1bml0dGVz\n" +
			"dEBrYWxlaWRvLmlvMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA1wgO\n" +
			"iHcCIBXkcDEhCUmylrbo6uPvA1BGSGES22F6tqG5ayujm5rZHQTlJLQxnlW3mn3x\n" +
			"BybKjeaWBJ/VgTHccWJELaL+Q1/Bm5sfCFK++DbaLAhto1luclY3TGIX/CBj1ELF\n" +
			"emWuS9lQV6OJHesANuQCmM/sWqNFhcR4qg0ybXlhr5fnuOCIp60Nsk33AeNz42FH\n" +
			"Kmr+FTtXbVaw0jHFbOr7Vy81+LBXsKrnawAoqyToLWbOZHy2EDuXKEzd+ZlCHmPH\n" +
			"kVLVmqavHm4bU2zuIMTsn8CdwpNgN5pOk3GWC0/4Pul/BcaIRs8pmwWE6+x12+n/\n" +
			"xgtQ0LtIYEj/fPstaQIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQDSVZLxNLrsuciQ\n" +
			"NIxbaBhjpilrOvGheKNZH6cSscPhfqyLSLrx1BumgB8Bp2aCxTv9zDh4ugUhrkEz\n" +
			"babAZJAlIfSD3IdwVFR4O2FBOLn73Ql1xoTqN1S2tersLzRy87BfDWxNIMQzwK5U\n" +
			"3I+xwCPCbtBrxZPULXT+fBlZjwCgC0MdKgq3aMsPLlPawSk1sT8BvQrn3o7dSe8q\n" +
			"kAhSssaP9XJDoV6saPMzjb+WUNZgI3uTw3nxbjr+rIM+C2KvPGS/+lpFfpGg0DMf\n" +
			"+eHpZMb2Vf1HzDxM1KGkpDI2McyVF6OxHJcITPY2GG2FKMnxg5Zj3Euzs8FDcg62\n" +
			"IjUBP/mt\n" +
			"-----END CERTIFICATE-----\n")
	testCertFile, _ := ioutil.TempFile("", "testcert")
	defer syscall.Unlink(testCertFile.Name())
	ioutil.WriteFile(testCertFile.Name(), testCertData, 0644)
	testCACertFile, _ := ioutil.TempFile("", "testca")
	defer syscall.Unlink(testCACertFile.Name())
	ioutil.WriteFile(testCACertFile.Name(), testCertData, 0644)

	mutualTLSArgs := []string{
		"-b", "broker1",
		"-t", "in-topic",
		"-T", "out-topic",
		"-g", "test-group",
		"-i", "clientid1",
		"--tls-enabled",
		"--tls-insecure",
		"--tls-clientkey", testPrivKeyFile.Name(),
		"--tls-clientcerts", testCertFile.Name(),
		"--tls-cacerts", testCACertFile.Name(),
	}

	f := NewMockKafkaFactory()
	_, err := execKafkaCommonWithArgs(assert, mutualTLSArgs, f)

	assert.Equal(nil, err)
	assert.Equal(true, f.ClientConf.Net.TLS.Enable)
	assert.Equal(1, len(f.ClientConf.Net.TLS.Config.Certificates))
	assert.Equal(1, len(f.ClientConf.Net.TLS.Config.RootCAs.Subjects()))
	assert.Equal(true, f.ClientConf.Net.TLS.Config.InsecureSkipVerify)
	assert.Equal("clientid1", f.ClientConf.ClientID) // generated UUID

	// Validate error handling
	syscall.Unlink(testCACertFile.Name())
	_, err = execKafkaCommonWithArgs(assert, mutualTLSArgs, f)
	assert.Regexp("no such file or directory", err.Error())

	syscall.Unlink(testCertFile.Name())
	_, err = execKafkaCommonWithArgs(assert, mutualTLSArgs, f)
	assert.Regexp("no such file or directory", err.Error())
}

func TestPrintForSaramaLogger(t *testing.T) {
	var l saramaLogger
	l.Print("Test log")
}

func TestPrintfForSaramaLogger(t *testing.T) {
	var l saramaLogger
	l.Printf("Test log")
}

func TestPrintlnForSaramaLogger(t *testing.T) {
	var l saramaLogger
	l.Println("Test log")
}

func TestConsumerErrorLoopLogsAndContinues(t *testing.T) {
	assert := assert.New(t)

	f := NewMockKafkaFactory()
	k, wg, err := startTestKafkaCommon(assert, kcMinWorkingArgs, f)
	if err != nil {
		return
	}

	f.Consumer.MockErrors <- fmt.Errorf("fizzle")

	// Shut down
	k.signals <- os.Interrupt
	wg.Wait()
}
