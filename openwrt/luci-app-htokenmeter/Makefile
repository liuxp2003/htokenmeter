# SPDX-License-Identifier: MIT
#
# Copyright (C) 2026 htokenmeter contributors

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-htokenmeter
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

PKG_MAINTAINER:=htokenmeter maintainers
PKG_CONFIG_DEPENDS:=

LUCI_TITLE:=LuCI support for htokenmeter AI gateway
LUCI_DEPENDS:=+htokenmeter
LUCI_PKGARCH:=all

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature
