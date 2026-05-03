# go-grpc-kit

## buf 에서 사용한 버전과 맞춰야 한다. (중요)
- google.golang.org/protobuf@v1.36.6
- google.golang.org/grpc@v1.64.1
- go mod 에서 google.golang.org/grpc v1.64.1 google.golang.org/protobuf v1.36.6 // indirect 만 수정해주고   
- 나머지 대표적으로 google.golang.org/genproto/googleapis/rpc v0.0.0-20250218202821-56aae31c358a // indirect  
- 같은 녀석들은 go mod tidy 로 해결 하면 될듯 하다. buf 에서는 일단 최신 googleapis 사용하도록 했으므로   

## todo
- server.go 에서 아래 코드를 주석 처리 했는데 service 에 대한 종속성이 발생했는데 이걸 해결 하도록 메서드를 수정할 필요가 있다.  
- Server() 수정해야함.  
- lint 추가 해줘야 함.  
- [x] 사용 설명 작성: [USAGE_GUIDE.md](USAGE_GUIDE.md) 를 참고하여 프레임워크가 아닌 툴킷 형태로 사용하도록 권장.  

```aiignore
    // service.RegisterDataBlockServiceServer(grpcServer)
    // service.RegisterDBApisServiceServer(grpcServer)
```
- 중요.  
- 아래 코드에서 "google.golang.org/grpc" 를 없앨 수 있는 방안을 찾아야 한다. 외부에서는 grpc 를 언급않하도록 해야 한다.  
- 여기서 처리하든, api-proto 에서 처리하든 처리 해야 함.  
```aiignore

package service

import (
	"context"
	"github.com/seoyhaein/tori/api"
	pb "github.com/seoyhaein/tori/protos"
	"google.golang.org/grpc"
)

//var Config = c.GlobalConfig

type dataBlockServiceServerImpl struct {
	pb.UnimplementedDataBlockServiceServer
	dbApis api.DBApis
}

// TODO 중요. 이거 반드시 처리 해야함.
// TODO NewDBApis 는 main 의 init 에서 config 처리를 함으로 테스트의 경우 에러 날 수 있음. 이거 보안하는 방향으로 처리 해야함.
// TODO init 처리 할때 main 에서만 처리하지 말고 다른 패키지에서 처리하는 방향을 생각해봐.

// NewDataBlockServiceServer 는 DataBlockServiceServer 의 새로운 인스턴스를 반환
func NewDataBlockServiceServer() pb.DataBlockServiceServer {
	return &dataBlockServiceServerImpl{
		dbApis: api.NewDBApis(),
	}
}

// RegisterDataBlockServiceServer server.go 에서 서비스 등록할때 사용하면 서비스 사용가능.
func RegisterDataBlockServiceServer(service *grpc.Server) {
	pb.RegisterDataBlockServiceServer(service, NewDataBlockServiceServer())
}

// GetDataBlock 는 클라이언트의 빈 요청에 또는 버전 요청에 대해서 DataBlock 또는 nil 을 반환한다.
func (s *dataBlockServiceServerImpl) GetDataBlock(ctx context.Context, in *pb.GetDataBlockRequest) (*pb.GetDataBlockResponse, error) {

	dataBlock, err := s.dbApis.GetDataBlock(ctx, in.CurrentUpdatedAt)
	if err != nil {
		return nil, err
	}
	// 클라이언트와 서버의 버전이 동일하다면 업데이트 할 필요 없음.
	if dataBlock == nil {
		return &pb.GetDataBlockResponse{
			Data:     nil,
			NoUpdate: true,
		}, nil
	}
	// 최신 버전 업데이트.
	return &pb.GetDataBlockResponse{
		Data:     dataBlock,
		NoUpdate: false,
	}, nil
}


```
- config.json 넣어두어야 함.

- grpc 테스트 환경 구축
- https://chatgpt.com/share/684acb4c-dbc0-800d-b2e5-cdbc02acedfe

- devcontainer.json

```
{
    "$schema": "https://raw.githubusercontent.com/devcontainers/spec/main/schemas/devContainer.base.schema.json",
    "name": "Go gRPC Dev Container",
    "build": {
        "dockerfile": "Dockerfile",
        "context": ".."
    },
    "runArgs": [
        "--cap-add=NET_ADMIN",   // tc(netem) 권한 부여
        "--network=host"        // 호스트 네트워크 네임스페이스 공유 (선택)
    ],
    "workspaceMount": "source=${localWorkspaceFolder},target=/app,type=bind,consistency=cached",
    "workspaceFolder": "/app",
    "remoteUser": "vscode",

    // VS Code 내 Go 지원, Docker 지원 확장 자동 설치
    "customizations": {
        "vscode": {
            "extensions": [
                "golang.go",
                "ms-azuretools.vscode-docker"
            ],
            // Go 모듈 자동완성 등 편의 설정
            "settings": {
                "go.useLanguageServer": true,
                "go.toolsManagement.autoUpdate": true
            }
        }
    },
    // 컨테이너 생성 후 의존성 설치
    "postCreateCommand": "go mod download",

    // gRPC 기본 포트 포워딩
    "forwardPorts": [
        50051
    ]
}
```

### 읽어보기
- https://benggri.tistory.com/137
- https://containers.dev/implementors/json_reference/#compose-specific
- https://github.com/grpc/grpc-go/tree/master/examples

### 반드시 처리하자.
- server_config 따로 뺐는데 config 따로할지 통합할지 고민해야 함. peernode 에서 필요한 config 도 있음. 연결할 서버 목롤을 코드에 넣을 수는 없음.
- dev container 관련 코드도 분석해야 함. client_test.go 소스 수정해야 함.