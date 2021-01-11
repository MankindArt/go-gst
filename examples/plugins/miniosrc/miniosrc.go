// This example demonstrates a src plugin that reads from objects in a minio bucket.
// Since minio implements the S3 API this plugin could also be used for S3 buckets by
// setting the correct endpoints and credentials.
//
// By default this plugin will use the credentials set in the environment at MINIO_ACCESS_KEY_ID
// and MINIO_SECRET_ACCESS_KEY however these can also be set on the element directly.
//
//
// In order to build the plugin for use by GStreamer, you can do the following:
//
//     $ go generate
//     $ go build -o libgstminiosrc.so -buildmode c-shared .
//
//
//go:generate gst-plugin-gen
//
// +plugin:Name=miniosrc
// +plugin:Description=GStreamer plugins for reading and writing from Minio
// +plugin:Version=v0.0.1
// +plugin:License=gst.LicenseLGPL
// +plugin:Source=go-gst
// +plugin:Package=examples
// +plugin:Origin=https://github.com/tinyzimmer/go-gst
// +plugin:ReleaseDate=2021-01-11
//
// +element:Name=miniosrc
// +element:Rank=gst.RankNone
// +element:Impl=minioSrc
// +element:Subclass=base.ExtendsBaseSrc
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/tinyzimmer/go-glib/glib"
	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/base"
)

func main() {}

const (
	accessKeyIDEnvVar     = "MINIO_ACCESS_KEY_ID"
	secretAccessKeyEnvVar = "MINIO_SECRET_ACCESS_KEY"
)

var (
	defaultEndpoint = "play.min.io"
	defaultUseTLS   = true
	defaultRegion   = "us-east-1"
	defaultSeekable = true
)

// CAT is the plugin log category
var CAT = gst.NewDebugCategory(
	"miniosrc",
	gst.DebugColorNone,
	"MinIOSrc Element",
)

var properties = []*gst.ParamSpec{
	gst.NewStringParam(
		"endpoint",
		"S3 API Endpoint",
		"The endpoint for the S3 API server",
		&defaultEndpoint,
		gst.ParameterReadWrite,
	),
	gst.NewBoolParam(
		"use-tls",
		"Use TLS",
		"Use HTTPS for API requests",
		defaultUseTLS,
		gst.ParameterReadWrite,
	),
	gst.NewStringParam(
		"region",
		"Bucket region",
		"The region where the bucket is",
		&defaultRegion,
		gst.ParameterReadWrite,
	),
	gst.NewStringParam(
		"bucket",
		"Bucket name",
		"The name of the bucket to fetch the obejct from",
		nil,
		gst.ParameterReadWrite,
	),
	gst.NewStringParam(
		"key",
		"Object key",
		"The key of the object to write to the stream",
		nil,
		gst.ParameterReadWrite,
	),
	gst.NewStringParam(
		"access-key-id",
		"Access Key ID",
		"The access key ID to use for authentication",
		nil,
		gst.ParameterReadWrite,
	),
	gst.NewStringParam(
		"secret-access-key",
		"Secret Access Key",
		"The secret access key to use for authentication",
		nil,
		gst.ParameterReadWrite,
	),
	gst.NewBoolParam(
		"seekable",
		"Enable Seeking",
		"Whether to report that seeking is enabled, disable over slow connections",
		defaultSeekable,
		gst.ParameterReadWrite,
	),
}

type settings struct {
	endpoint        string
	useTLS          bool
	region          string
	bucket          string
	key             string
	accessKeyID     string
	secretAccessKey string
	seekable        bool
}

func defaultSettings() *settings {
	return &settings{
		endpoint:        defaultEndpoint,
		useTLS:          defaultUseTLS,
		region:          defaultRegion,
		accessKeyID:     os.Getenv(accessKeyIDEnvVar),
		secretAccessKey: os.Getenv(secretAccessKeyEnvVar),
		seekable:        defaultSeekable,
	}
}

type state struct {
	started bool
	object  *minio.Object
	objInfo minio.ObjectInfo

	mux sync.Mutex
}

type minioSrc struct {
	settings *settings
	state    *state
}

func (m *minioSrc) New() gst.GoElement {
	CAT.Log(gst.LevelLog, "Creating new minioSrc object")
	return &minioSrc{
		settings: defaultSettings(),
		state:    &state{},
	}
}

func (m *minioSrc) TypeInit(*gst.TypeInstance) {}

func (m *minioSrc) ClassInit(klass *gst.ElementClass) {
	CAT.Log(gst.LevelLog, "Initializing miniosrc class")
	klass.SetMetadata(
		"MinIO Source",
		"Source/File",
		"Read stream from a MinIO object",
		"Avi Zimmerman <avi.zimmerman@gmail.com>",
	)
	CAT.Log(gst.LevelLog, "Adding src pad template and properties to class")
	klass.AddPadTemplate(gst.NewPadTemplate(
		"src",
		gst.PadDirectionSource,
		gst.PadPresenceAlways,
		gst.NewAnyCaps(),
	))
	klass.InstallProperties(properties)
}

func (m *minioSrc) SetProperty(self *gst.Object, id uint, value *glib.Value) {
	prop := properties[id]

	val, err := value.GoValue()
	if err != nil {
		gst.ToElement(self).ErrorMessage(gst.DomainLibrary, gst.LibraryErrorSettings,
			fmt.Sprintf("Could not coerce %v to go value", value), err.Error())
	}

	switch prop.Name() {
	case "endpoint":
		m.settings.endpoint = val.(string)
	case "use-tls":
		m.settings.useTLS = val.(bool)
	case "region":
		m.settings.region = val.(string)
	case "bucket":
		m.settings.bucket = val.(string)
	case "key":
		m.settings.key = val.(string)
	case "access-key-id":
		m.settings.accessKeyID = val.(string)
	case "secret-access-key":
		m.settings.secretAccessKey = val.(string)
	case "seekable":
		m.settings.seekable = val.(bool)
	}

}

func (m *minioSrc) GetProperty(self *gst.Object, id uint) *glib.Value {
	prop := properties[id]

	var localVal interface{}

	switch prop.Name() {
	case "endpoint":
		localVal = m.settings.endpoint
	case "use-tls":
		localVal = m.settings.useTLS
	case "region":
		localVal = m.settings.region
	case "bucket":
		localVal = m.settings.bucket
	case "key":
		localVal = m.settings.key
	case "access-key-id":
		localVal = m.settings.accessKeyID
	case "secret-access-key":
		localVal = m.settings.secretAccessKey
	case "seekable":
		localVal = m.settings.seekable

	default:
		gst.ToElement(self).ErrorMessage(gst.DomainLibrary, gst.LibraryErrorSettings,
			fmt.Sprintf("Cannot get invalid property %s", prop.Name()), "")
		return nil
	}

	val, err := glib.GValue(localVal)
	if err != nil {
		gst.ToElement(self).ErrorMessage(gst.DomainLibrary, gst.LibraryErrorFailed,
			fmt.Sprintf("Could not convert %v to GValue", localVal),
			err.Error(),
		)
	}

	return val
}

func (m *minioSrc) Constructed(self *gst.Object) {
	self.Log(CAT, gst.LevelLog, "Setting format of GstBaseSrc to bytes")
	base.ToGstBaseSrc(self).SetFormat(gst.FormatBytes)
}

func (m *minioSrc) IsSeekable(*base.GstBaseSrc) bool { return m.settings.seekable }

func (m *minioSrc) GetSize(self *base.GstBaseSrc) (bool, int64) {
	if !m.state.started {
		return false, 0
	}
	return true, m.state.objInfo.Size
}

func (m *minioSrc) Start(self *base.GstBaseSrc) bool {
	m.state.mux.Lock()

	if m.state.started {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorFailed, "MinIOSrc is already started", "")
		return false
	}

	if m.settings.bucket == "" {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorFailed, "No source bucket defined", "")
		return false
	}

	if m.settings.key == "" {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorFailed, "No object key defined", "")
		return false
	}

	client, err := minio.New(m.settings.endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(m.settings.accessKeyID, m.settings.secretAccessKey, ""),
		Secure: m.settings.useTLS,
	})

	if err != nil {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorFailed,
			fmt.Sprintf("Failed to connect to MinIO endpoint %s", m.settings.endpoint), err.Error())
		return false
	}

	self.Log(CAT, gst.LevelInfo, fmt.Sprintf("Requesting %s/%s from %s", m.settings.bucket, m.settings.key, m.settings.endpoint))
	m.state.object, err = client.GetObject(context.Background(), m.settings.bucket, m.settings.key, minio.GetObjectOptions{})
	if err != nil {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorOpenRead,
			fmt.Sprintf("Failed to retrieve object %q from bucket %q", m.settings.key, m.settings.bucket), err.Error())
		return false
	}

	self.Log(CAT, gst.LevelInfo, "Getting HEAD for object")
	m.state.objInfo, err = m.state.object.Stat()
	if err != nil {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorOpenRead,
			fmt.Sprintf("Failed to stat object %q in bucket %q: %s", m.settings.key, m.settings.bucket, err.Error()), "")
		return false
	}
	self.Log(CAT, gst.LevelInfo, fmt.Sprintf("%+v", m.state.objInfo))

	m.state.started = true

	m.state.mux.Unlock()

	self.StartComplete(gst.FlowOK)

	self.Log(CAT, gst.LevelInfo, "MinIOSrc has started")
	return true
}

func (m *minioSrc) Stop(self *base.GstBaseSrc) bool {
	m.state.mux.Lock()
	defer m.state.mux.Unlock()

	if !m.state.started {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorSettings, "MinIOSrc is not started", "")
		return false
	}

	if err := m.state.object.Close(); err != nil {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorClose, "Failed to close the bucket object", err.Error())
		return false
	}

	m.state.object = nil
	m.state.started = false

	self.Log(CAT, gst.LevelInfo, "MinIOSrc has stopped")
	return true
}

func (m *minioSrc) Fill(self *base.GstBaseSrc, offset uint64, size uint, buffer *gst.Buffer) gst.FlowReturn {

	if !m.state.started || m.state.object == nil {
		self.ErrorMessage(gst.DomainCore, gst.CoreErrorFailed, "MinIOSrc is not started yet", "")
		return gst.FlowError
	}

	self.Log(CAT, gst.LevelLog, fmt.Sprintf("Request to fill buffer from offset %v with size %v", offset, size))

	m.state.mux.Lock()
	defer m.state.mux.Unlock()

	data := make([]byte, size)
	read, err := m.state.object.ReadAt(data, int64(offset))
	if err != nil && err != io.EOF {
		self.ErrorMessage(gst.DomainResource, gst.ResourceErrorRead,
			fmt.Sprintf("Failed to read %d bytes from object at offset %d", size, offset), err.Error())
		return gst.FlowError
	}

	if read < int(size) {
		self.Log(CAT, gst.LevelDebug, fmt.Sprintf("Only read %d bytes from object, trimming", read))
		trim := make([]byte, read)
		copy(trim, data)
		data = trim
	}

	bufmap := buffer.Map(gst.MapWrite)
	if bufmap == nil {
		self.ErrorMessage(gst.DomainLibrary, gst.LibraryErrorFailed, "Failed to map buffer", "")
		return gst.FlowError
	}
	defer buffer.Unmap()

	bufmap.WriteData(data)
	buffer.SetSize(int64(read))

	return gst.FlowOK
}
