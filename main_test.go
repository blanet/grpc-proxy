package main

import (
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/assert"

	"gitlab.alibaba-inc.com/moweng.xx/grpc-proxy/example"
)

func Test_ProtoMessageBytesKeepUnchanged(t *testing.T) {
	// old version of pb
	demoMsgV1 := &example.DemoMsgV1{
		Name: "original name",
		Age:  100,
	}
	bytesV1, err := proto.Marshal(demoMsgV1)
	assert.NoError(t, err)

	// unmarshal to a upgraded proto message
	demoMsgV2 := &example.DemoMsgV2{}
	err = proto.Unmarshal(bytesV1, demoMsgV2)
	assert.NoError(t, err)

	// marshal the new message
	bytesV2, err := proto.Marshal(demoMsgV2)
	assert.NoError(t, err)
	assert.Equal(t, bytesV1, bytesV2)

	// change message for v2
	demoMsgV2.Name = "changed name"
	demoMsgV2.Address = "this is a message"
	bytesV2Changed, err := proto.Marshal(demoMsgV2)
	assert.NoError(t, err)

	// use bytes for v2 to unmarshal message v1 again
	demoMsgV1Final := &example.DemoMsgV1{}
	err = proto.Unmarshal(bytesV2Changed, demoMsgV1Final)
	assert.NoError(t, err)
	assert.Equal(t, int32(100), demoMsgV1Final.Age)
	assert.Equal(t, "changed name", demoMsgV1Final.Name)
}
