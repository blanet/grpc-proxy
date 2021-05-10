package main

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strconv"

	"github.com/golang/protobuf/proto"

	pb "gitlab.alibaba-inc.com/agit/satellite-proto/go"
)

// matches a tag string like "bytes,1,opt,name=repository,proto3"
var protobufTagRegex = regexp.MustCompile(`^(.*?),(\d+),(.*?),name=(.*?),proto3(\,oneof)?$`)

/*
message Repository {
    reserved 1;
    reserved "path";
    string storage_name = 2;
    string relative_path = 3;
    string git_object_directory = 4;
    repeated string git_alternate_object_directories = 5;
    string gl_repository = 6;
    int64 gl_repository_id = 7;
    string gl_project_path = 8;
    repeated string configs = 9;
}
*/

func main() {
	msg := proto.Message(&pb.GetBlobRequest{
		Repository: &pb.Repository{
			StorageName:    "git",
			RelativePath:   "demo/repo.git",
			GlRepositoryId: 100,
			Configs:        []string{"this=that", "here=there"},
		},
		Oid:   "demo_oid",
		Limit: 1000,
	})

	// 01. raw message on the wire
	raw, err := proto.Marshal(msg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("message name: %s\n", proto.MessageName(msg))
	fmt.Printf("message onwire: %v\n", raw)

	// 02. we need to find a way to change the raw message on the wire
	mt := proto.MessageType("satellite.GetBlobRequest")
	if mt == nil {
		log.Fatal("no message type found")
	}

	m := reflect.New(mt.Elem())
	mm, ok := m.Interface().(proto.Message)
	if !ok {
		log.Fatal(fmt.Errorf("%T is not a protobuf message", m.Interface()))
	}

	if err := proto.Unmarshal(raw, mm); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("message type: %v\n", mm.String())

	// 03. get repository from parent proto message
	msgV := reflect.ValueOf(mm)
	if msgV.IsZero() {
		log.Fatal("proto field is empty")
	}

	msgV = reflect.Indirect(msgV)
	var repositoryField reflect.Value
	for i := 0; i < msgV.NumField(); i++ {
		field := msgV.Type().Field(i)
		tag := field.Tag.Get("protobuf")
		matches := protobufTagRegex.FindStringSubmatch(tag)
		if len(matches) == 6 {
			fieldStr := matches[2]
			if fieldStr == strconv.Itoa(1) {
				repositoryField = msgV.FieldByName(field.Name)
				break
			}
		}
		fmt.Println(tag)
	}

	repo, ok := repositoryField.Interface().(*pb.Repository)
	if !ok {
		log.Fatal("err finding repository")
	}
	fmt.Printf("fount repo: %v\n", repo.String())
	repo.StorageName = "changed_here"

	fmt.Printf("changed full msg: %v\n", mm.String())
}
