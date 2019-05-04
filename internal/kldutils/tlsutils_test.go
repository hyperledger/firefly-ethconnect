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

package kldutils

import (
	"io/ioutil"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

func TestCreateTLSConfigurationWithDefaultOptions(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	tlsConfigOptions := TLSConfig{}
	tlsConfig, err := CreateTLSConfiguration(&tlsConfigOptions)

	assert.Nil(err)
	assert.Nil(tlsConfig)

}

func TestCreateTLSConfigurationWithInvalMutalAuth(t *testing.T) {
	assert := assert.New(t)

	tlsConfigOptions := TLSConfig{
		ClientCertsFile: "anything",
	}
	_, err := CreateTLSConfiguration(&tlsConfigOptions)

	assert.Regexp("Client private key and certificate must both be provided for mutual auth", err.Error())
}

func TestCreateTLSConfigurationWithSelfSignedMutualAuth(t *testing.T) {
	log.SetLevel(log.DebugLevel)
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

	tlsConfigOptions := TLSConfig{
		Enabled:            true,
		InsecureSkipVerify: true,
		ClientKeyFile:      testPrivKeyFile.Name(),
		ClientCertsFile:    testCertFile.Name(),
		CACertsFile:        testCACertFile.Name(),
	}
	tlsConfig, err := CreateTLSConfiguration(&tlsConfigOptions)

	assert.Equal(nil, err)
	assert.Equal(1, len(tlsConfig.Certificates))
	assert.Equal(1, len(tlsConfig.RootCAs.Subjects()))
	assert.Equal(true, tlsConfig.InsecureSkipVerify)

	// Validate error handling
	syscall.Unlink(testCACertFile.Name())
	tlsConfig, err = CreateTLSConfiguration(&tlsConfigOptions)
	assert.Regexp("no such file or directory", err.Error())

	syscall.Unlink(testCertFile.Name())
	tlsConfig, err = CreateTLSConfiguration(&tlsConfigOptions)
	assert.Regexp("no such file or directory", err.Error())
}
