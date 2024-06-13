# open-im-server
open-im-server  v3.6版本，支持单独编译和启动服务

指定编译：

```
make build BINS=`"openim-api openim-crontask"
````

指定开始
```
make start `specificServers="openim-api openim-crontask"`
```
停止服务
```
make stop
```
目前停止服务不支持指定服务，只能全部停止
