## open-im-server
open-im-server v3.6版本，支持单独编译和启动服务

指定编译：
```
make build BINS=`"openim-api openim-crontask"
```
指定开始

```
make start `specificServers="openim-api openim-crontask"`
```
停止服务
```
make stop
```
目前停止服务不支持指定服务，只能全部停止

调整的相关文件：
![image](https://github.com/wesley-24-1538/root-open-im-server/assets/169232774/e5e89586-309f-4a42-b2a5-e5e11dd7d9a7)

先使用docker-composer.yml启动其他服务
再使用源码编译启动本项目

使用示例环境文件：
environment.sh.example 改名 environment.sh覆盖项目同名文件
要保证本项目和其他项目能够相互通讯
