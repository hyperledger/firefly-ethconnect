// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldkvstore

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func tempdir(t *testing.T) string {
	dir, _ := ioutil.TempDir("", "kld")
	t.Logf("tmpdir/create: %s", dir)
	return dir
}

func cleanup(t *testing.T, dir string) {
	t.Logf("tmpdir/cleanup: %s [dir]", dir)
	os.RemoveAll(dir)
}

func TestLevelDBPutGet(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	kv, err := NewLDBKeyValueStore(path.Join(dir, "db"))
	assert.NoError(err)
	err = kv.Put("things", []byte("stuff"))
	assert.NoError(err)
	things, err := kv.Get("things")
	assert.NoError(err)
	assert.Equal("stuff", string(things))
	err = kv.Delete("things")
	assert.NoError(err)
	kv.Close()
}

func TestLevelDBIterate(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	kv, err := NewLDBKeyValueStore(path.Join(dir, "db"))
	assert.NoError(err)
	for i := 0; i < 100; i++ {
		err = kv.Put(fmt.Sprintf("key_%.3d", i), []byte(fmt.Sprintf("val_%.3d", i)))
		assert.NoError(err)
	}
	it := kv.NewIterator()
	j := 0
	for it.Next() {
		assert.Equal(fmt.Sprintf("key_%.3d", j), it.Key())
		assert.Equal([]byte(fmt.Sprintf("val_%.3d", j)), it.Value())
		j++
	}
	it.Release()
	kv.Close()
}

func TestLevelDBBadPath(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	dbPath := path.Join(dir, "badness")
	ioutil.WriteFile(dbPath, []byte{}, 0644)
	_, err := NewLDBKeyValueStore(dbPath)
	assert.Regexp("Failed to open DB", err.Error())
}

func TestLevelDBWarnIfError(t *testing.T) {
	db := &levelDBKeyValueStore{}
	db.warnIfErr("Put", "A Key", fmt.Errorf("pop"))
}
