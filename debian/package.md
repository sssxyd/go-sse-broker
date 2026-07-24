安装打包依赖
sudo apt update
sudo apt install -y build-essential devscripts debhelper fakeroot golang-go lintian

在项目根目录先编译二进制（你的 install 文件会打包这个文件）
cd /path/to/go-sse-broker
go build -trimpath -ldflags="-s -w" -o sse-broker .

确认打包脚本可执行
chmod +x debian/rules debian/postinst debian/prerm debian/postrm

打二进制包
dpkg-buildpackage -us -uc -b

产物位置
会在上级目录生成类似：
../sse-broker_版本_amd64.deb

安装验证
sudo dpkg -i ../sse-broker_版本_amd64.deb
sudo systemctl status sse-broker