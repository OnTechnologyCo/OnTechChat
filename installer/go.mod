module ontechdev.com/ontechchat/installer

go 1.26.0

require (
	ontechdev.com/ontechchat/server v0.0.0
	ontechdev.com/ontechchat/tray v0.0.0
)

require (
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace ontechdev.com/ontechchat/server => ../server

replace ontechdev.com/ontechchat/tray => ../tray
