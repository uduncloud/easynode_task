package ether

import (
	"errors"
	"github.com/sirupsen/logrus"
	"github.com/sunjiangjun/xlog"
	"github.com/tidwall/gjson"
	"github.com/uduncloud/easynode_task/config"
	"github.com/uduncloud/easynode_task/net/ether"
	"github.com/uduncloud/easynode_task/util"
	"time"
)

type Ether struct {
	log *xlog.XLog
}

func NewEther(log *xlog.XLog) *Ether {
	return &Ether{
		log: log,
	}
}

func (e *Ether) GetLastBlockNumber(v *config.BlockConfig) (int64, error) {
	log := e.log.WithFields(logrus.Fields{
		"id":    time.Now().UnixMilli(),
		"model": "GetLastBlockNumber",
	})
	var lastNumber int64
	jsonResult, err := ether.Eth_GetBlockNumber(v.NodeHost, v.NodeKey)
	if err != nil {
		log.Errorf("Eth_GetBlockNumber|err=%v", err)
		return 0, err
	} else {
		log.Printf("Eth_GetBlockNumber|resp=%v", jsonResult)
	}

	//获取链的最新区块高度
	number := gjson.Parse(jsonResult).Get("result").String()
	lastNumber, err = util.HexToInt(number)
	if err != nil {
		log.Errorf("HexToInt|err=%v", err)
		return 0, err
	}
	if lastNumber > 1 {
		//_ = s.UpdateLastNumber(v.BlockChainCode, lastNumber)
		return lastNumber, nil
	} else {
		return 0, errors.New("GetLastBlockNumber is fail")
	}
}
