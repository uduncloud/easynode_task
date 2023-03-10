package monitor

import (
	"github.com/uduncloud/easynode_task/config"
	"testing"
)

func Init() *Service {
	cfg := config.LoadConfig("./../../config.json")
	return NewService(&cfg)
}

func TestService_HandlerDeadTask(t *testing.T) {
	s := Init()
	s.HandlerDeadTask()

	//log.Println(time.Now().Add(-1 * time.Hour).UTC())
}

func TestService_HandlerManyFailTask(t *testing.T) {
	s := Init()
	s.HandlerManyFailTask()
	//s.createNodeTaskTable()
}

func TestService_CheckBlockAndTx(t *testing.T) {
	s := Init()
	s.CheckBlockAndTx(s.config.BlockConfigs[0])
}

func TestService_DelNodeErrorWithReceiptByBlockNumber(t *testing.T) {
	s := Init()
	_ = s.DelNodeErrorWithReceiptByBlockNumber("16045911", s.config.BlockConfigs[0])
}
