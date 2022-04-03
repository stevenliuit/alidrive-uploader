package bootstrap

import (
	"alidrive_uploader/conf"
	"alidrive_uploader/pkg/alidrive"
	"alidrive_uploader/pkg/util"
	"github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb/v7"
	"math"
	"os"
	"path/filepath"
	"sync"
)

var errors = map[string]string{}

func Run() {
	var err error

	InitFlags()
	InitConfig()
	InitLog()

	stat, err := os.Stat(conf.Opt.Positional.LocalPath)
	if err != nil {
		logrus.Panic(err)
		return
	}
	var allFiles []string
	if stat.IsDir() {
		allFiles, err = util.GetAllFiles(conf.Opt.Positional.LocalPath)
		if err != nil {
			logrus.Panic(err)
			return
		}
	} else {
		allFiles = []string{conf.Opt.Positional.LocalPath}
	}

	drive := alidrive.AliDrive{
		Instance: alidrive.Instance{
			RefreshToken: conf.Conf.AliDrive.RefreshToken,
			DriveId:      conf.Conf.AliDrive.DriveId,
			AccessToken:  "",
			ParentPath:   "root",
		},
	}
	logrus.Infof("正在获取AccessToken")
	if err := drive.RefreshToken(); err != nil {
		logrus.Panic(err)
		return
	}
	conf.SaveConfig()

	logrus.Infof("正在获取目录")
	var files []util.FileStream
	//建立目录结构
	var dirs = make(map[string]string, 0)
	for _, fp := range allFiles {
		//目录
		dir := filepath.Dir(fp)
		file, err := readFileInfo(fp)
		if err != nil {
			logrus.Panic(err)
			return
		}
		file.ParentPath = dir
		files = append(files, file)
		dirs[dir] = ""
	}
	if len(files) <= 0 {
		return
	}

	TreeFolders(&drive, conf.Opt.Positional.RemotePath, dirs)

	wg := sync.WaitGroup{}
	p := mpb.New(mpb.WithWaitGroup(&wg))

	//文件数量进度条
	taskBar := util.NewMpbTask(p, "任务列表", int64(len(files)))

	workersNum := int(math.Min(float64(len(files)), float64(*conf.Opt.Transfers)))
	jobs := make(chan util.FileStream, workersNum)

	for i := 0; i < workersNum; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transfer(jobs, taskBar, p, &drive, dirs)
		}()
	}
	for _, file := range files {
		jobs <- file
	}
	close(jobs)
	p.Wait()
	logrus.Infof("上传完毕！共计%d个文件，失败文件个数：%d个", len(files), len(errors))
}

func transfer(jobs chan util.FileStream, taskBar *mpb.Bar, p *mpb.Progress, drive *alidrive.AliDrive, dirs map[string]string) {

	for file := range jobs {
		logrus.Infof("[%v]正在上传", file.Name)
		bar := util.NewMpb(p, file.Name, int64(file.Size))
		file.Bar = bar.ProxyReader(file.File)
		file.ParentPath = dirs[file.ParentPath]
		err := drive.Upload(file)
		if err != nil {
			logrus.Errorf("[%v]上传失败", err)
			errors[file.ReadlPath] = err.Error()
		} else {
			logrus.Infof("[%v]上传成功", file.Name)
		}
		taskBar.Increment()
		_ = file.Bar.Close()
	}
}
func readFileInfo(fp string) (util.FileStream, error) {

	var fs util.FileStream
	open, err := os.Open(fp)
	if err != nil {
		return fs, err
	}
	stat, err := os.Stat(fp)

	if err != nil {
		return fs, err
	}
	contentType, err := util.GetFileContentType(open)
	if err != nil {
		return fs, err
	}
	abs, err := filepath.Abs(fp)

	if err != nil {
		return fs, err
	}
	return util.FileStream{
		File:       open,
		Size:       uint64(stat.Size()),
		ParentPath: "root",
		Name:       stat.Name(),
		MIMEType:   contentType,
		ReadlPath:  abs,
	}, nil
}