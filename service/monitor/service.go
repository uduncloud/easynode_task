package monitor

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/sunjiangjun/xlog"
	"github.com/uduncloud/easynode_task/common/sql"
	"github.com/uduncloud/easynode_task/config"
	"github.com/uduncloud/easynode_task/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

/**
  1. 判断任务长时间处于 task_status=3,则 直接改成2（失败）
  2. 如果一个任务 多次失败，则不在重试
*/

type Service struct {
	config       *config.Config
	nodeSourceDb *gorm.DB
	taskDb       *gorm.DB
	nodeInfoDb   *gorm.DB
	nodeErrorDb  *gorm.DB
	log          *xlog.XLog
}

func NewService(config *config.Config) *Service {
	xg := xlog.NewXLogger().BuildOutType(xlog.FILE).BuildFile("./log/task/monitor_task", 24*time.Hour)
	s, err := sql.Open(config.NodeSourceDb.User, config.NodeSourceDb.Password, config.NodeSourceDb.Addr, config.NodeSourceDb.DbName, config.NodeSourceDb.Port, xg)
	if err != nil {
		panic(err)
	}

	info, err := sql.Open(config.NodeInfoDb.User, config.NodeInfoDb.Password, config.NodeInfoDb.Addr, config.NodeInfoDb.DbName, config.NodeInfoDb.Port, xg)
	if err != nil {
		panic(err)
	}

	task, err := sql.Open(config.NodeTaskDb.User, config.NodeTaskDb.Password, config.NodeTaskDb.Addr, config.NodeTaskDb.DbName, config.NodeTaskDb.Port, xg)
	if err != nil {
		panic(err)
	}

	nodeErr, err := sql.Open(config.NodeErrorDb.User, config.NodeErrorDb.Password, config.NodeErrorDb.Addr, config.NodeErrorDb.DbName, config.NodeErrorDb.Port, xg)
	if err != nil {
		panic(err)
	}

	return &Service{
		config:       config,
		nodeSourceDb: s,
		nodeErrorDb:  nodeErr,
		nodeInfoDb:   info,
		taskDb:       task,
		log:          xg,
	}
}

func (s *Service) Start() {

	//每日分表
	go s.createNodeTaskTable()

	//定时处理异常数据
	go func() {
		for true {
			<-time.After(30 * time.Minute)
			//处理僵死任务：即 长期处理进行中 status=3
			s.HandlerDeadTask()

			//处理任务失败多次的
			s.HandlerManyFailTask()

			//失败任务重试
			s.RetryTaskForFail()

		}
	}()
}

func (s *Service) createNodeTaskTable() {
	for true {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 23, 50, 0, 0, now.Location())
		<-time.After(next.Sub(now))

		log := s.log.WithFields(logrus.Fields{
			"id":    time.Now().UnixMilli(),
			"model": "createNodeTaskTable",
		})
		//new next table
		createSql := "CREATE TABLE if NOT EXISTS `%v` (\n  `node_id` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL COMMENT '节点的唯一标识',\n  `block_number` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci DEFAULT NULL COMMENT '区块高度',\n  `block_hash` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci DEFAULT NULL COMMENT '区块hash',\n  `tx_hash` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci DEFAULT NULL COMMENT '交易hash',\n  `task_type` tinyint NOT NULL DEFAULT '0' COMMENT ' 0:保留 1:同步Tx 2:同步Block 3:同步收据',\n  `block_chain` int NOT NULL DEFAULT '100' COMMENT '公链code, 默认：100 (etc)',\n  `task_status` int DEFAULT '0' COMMENT '0: 初始 1: 成功. 2: 失败.  3: 执行中. 4:kafka 写入中 5:重试 其他：重试次数(5以上)',\n  `create_time` timestamp NULL DEFAULT CURRENT_TIMESTAMP,\n  `log_time` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  PRIMARY KEY (`id`),\n  KEY `type` (`task_type`) USING BTREE,\n  KEY `status` (`task_status`) USING BTREE,\n  KEY `tx_hash` (`tx_hash`) USING BTREE,\n  KEY `block_number` (`block_number`) USING BTREE,\n  KEY `block_hash` (`block_hash`) USING BTREE, \n KEY `block_chain` (`block_chain`) USING BTREE,\n  KEY `node_id` (`node_id`) USING BTREE\n) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='节点任务表';"
		day := next.Add(5 * time.Hour).Format(service.DayFormat)
		pre := next.Add(-5 * time.Hour).Format(service.DayFormat)

		dayTable := fmt.Sprintf("%v_%v", s.config.NodeTaskDb.Table, day)
		preTable := fmt.Sprintf("%v_%v", s.config.NodeTaskDb.Table, pre)

		createSql = fmt.Sprintf(createSql, dayTable)
		err := s.taskDb.Exec(createSql).Error
		if err != nil {
			log.Errorf("task.exec|sql=%v,error=%v", createSql, err)
		}

		next = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		<-time.After(next.Sub(time.Now()))

		//cp data from pre table to current table
		cpSql := `
INSERT IGNORE INTO %v (id, node_id, block_number, block_hash, tx_hash, task_type, block_chain, task_status ) SELECT
id,
node_id,
block_number,
block_hash,
tx_hash,
task_type,
block_chain,
task_status 
FROM %v where task_status in (0,2,3,4)
`
		cpSql = fmt.Sprintf(cpSql, dayTable, preTable)

		err = s.taskDb.Exec(cpSql).Error
		if err != nil {
			log.Errorf("taskDb.Exec|sql=%v,error=%v", cpSql, err)
		}

		//delete pre table
		dropSql := "drop table %v"
		dropSql = fmt.Sprintf(dropSql, preTable)
		err = s.taskDb.Exec(dropSql).Error
		if err != nil {
			log.Printf("taskDb.Exec|sql=%v,error=%v", dropSql, err)
		}
		//delete binlog
		s.taskDb.Exec("RESET MASTER")

	}
}

func (s *Service) getNodeTaskTable() string {
	table := fmt.Sprintf("%v_%v", s.config.NodeTaskDb.Table, time.Now().Format(service.DayFormat))
	return table
}

//RetryTaskForFail 针对失败的任务，重发任务
func (s *Service) RetryTaskForFail() {
	log := s.log.WithFields(logrus.Fields{
		"id":    time.Now().UnixMilli(),
		"model": "RetryTaskForFail",
	})

	var ids []int64
	err := s.taskDb.Table(s.getNodeTaskTable()).Select("id").Where("task_status=?", 2).Pluck("id", &ids).Error
	if err != nil {
		log.Errorf("taskDb|err=%v", err)
		return
	}

	if len(ids) > 0 {

		sqlStr := `
 INSERT IGNORE INTO %v(block_chain,tx_hash,block_hash,block_number,source_type)
SELECT block_chain,tx_hash,block_hash,block_number,CASE 
	WHEN task_type=1 THEN
		1
	WHEN task_type=2 THEN
	2
	ELSE
		3
END as source_type FROM %v WHERE task_status=2 and id in (?)
`
		sqlStr = fmt.Sprintf(sqlStr, s.config.NodeSourceDb.Table, s.getNodeTaskTable())
		err = s.nodeSourceDb.Exec(sqlStr, ids).Error
		if err != nil {
			log.Errorf("nodeSourceDb|sql=%v,error=%v", sqlStr, err)
		}

		str2 := `UPDATE %v SET task_status=5 WHERE task_status=2 and id in (?)`
		str2 = fmt.Sprintf(str2, s.getNodeTaskTable())
		err = s.taskDb.Exec(str2, ids).Error
		if err != nil {
			log.Errorf("taskDb|sql=%v,error=%v", str2, err)
		}
	}
}

func (s *Service) HandlerDeadTask() {
	log := s.log.WithFields(logrus.Fields{
		"id":    time.Now().UnixMilli(),
		"model": "HandlerDeadTask",
	})
	//长时间"正在执行"的任务，改成失败状态
	err := s.taskDb.Table(s.getNodeTaskTable()).Where("task_status in (?) and create_time<?", []int{3, 4}, time.Now().Add(-30*time.Minute).UTC().Format("2006-01-02 15:04:05")).UpdateColumn("task_status", 2).Error
	if err != nil {
		log.Errorf("taskDb|update|err=%v", err.Error())
	}
}

func (s *Service) HandlerManyFailTask() {
	log := s.log.WithFields(logrus.Fields{
		"id":    time.Now().UnixMilli(),
		"model": "HandlerManyFailTask",
	})

	//如果任务多次重试，仍然失败，则放弃
	str := `
SELECT block_chain, block_number,block_hash,tx_hash,task_type,count(1) as c FROM %v WHERE task_status in (2,5) GROUP BY block_chain, block_number,block_hash,tx_hash,task_type HAVING c>?
`
	str = fmt.Sprintf(str, s.getNodeTaskTable())
	var list []*service.NodeTask
	err := s.taskDb.Raw(str, 5).Scan(&list).Error
	if err != nil {
		log.Printf("taskDb|raw|sql=%v,err=%v", str, err)
		return
	}

	for _, v := range list {
		err := s.taskDb.Table(s.getNodeTaskTable()).Where("block_chain=? and block_number=? and block_hash=? and tx_hash=? and task_type=?", v.BlockChain, v.BlockNumber, v.BlockHash, v.TxHash, v.TaskType).UpdateColumn("task_status", 5).Error
		if err != nil {
			log.Errorf("taskDb|update|err=%v", err)
			continue
		}
	}
}

func (s *Service) AddNodeError(list []*service.NodeSource) error {
	err := s.nodeErrorDb.Table(s.config.NodeErrorDb.Table).Clauses(clause.Insert{Modifier: "IGNORE"}).Omit("id,create_time").CreateInBatches(&list, 10).Error
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) DelNodeErrorWithBlockByBlockNumber(number string, chain *config.BlockConfig) error {

	delSql := "delete from %v where block_chain=? and block_number=? and source_type=?"
	delSql = fmt.Sprintf(delSql, s.config.NodeErrorDb.Table)
	err := s.nodeSourceDb.Exec(delSql, chain.BlockChainCode, number, 2).Error
	//err := s.nodeErrorDb.Table().Where("block_chain=? and block_number=? and source_type=?", chain.BlockChainCode, number, 2).Delete(s.config.NodeErrorDb.Table).Error
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) DelNodeErrorWithReceiptByBlockNumber(number string, chain *config.BlockConfig) error {
	delSql := "delete from %v where block_chain=? and block_number=? and source_type=?"
	delSql = fmt.Sprintf(delSql, s.config.NodeErrorDb.Table)
	err := s.nodeSourceDb.Exec(delSql, chain.BlockChainCode, number, 3).Error
	//err := s.nodeErrorDb.Where("block_chain=? and block_number=? and source_type=?", chain.BlockChainCode, number, 3).Delete(s.config.NodeErrorDb.Table).Error
	if err != nil {
		return err
	}
	return nil
}
