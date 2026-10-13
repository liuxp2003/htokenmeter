# SPDX-License-Identifier: GPL-3.0-or-later
#
# Copyright (C) 2026 htokenmeter contributors

include $(TOPDIR)/rules.mk

PKG_NAME:=htokenmeter
PKG_VERSION:=0.1.0
PKG_RELEASE:=4

# OpenAppFilter convention: sources live in this directory (repo-as-feed —
# clone the repo into openwrt/package/ and the package builds from the
# in-tree sources below; no PKG_SOURCE download is involved).
PKG_LICENSE:=GPL-3.0-or-later
PKG_MAINTAINER:=htokenmeter maintainers

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

GO_PKG:=github.com/lxp/htokenmeter
GO_PKG_LDFLAGS_X:=github.com/lxp/htokenmeter/common.Version=$(PKG_VERSION)
GO_PKG_BUILD_PKG:=github.com/lxp/htokenmeter
# NOTE: golang-package.mk automatically appends "-X $(GO_PKG_LDFLAGS_X)" to
# GO_PKG_LDFLAGS — do NOT inline $(GO_PKG_LDFLAGS_X) here; a bare
# "importpath.name=value" token is an invalid linker flag and kills the
# final link with the linker usage dump.
GO_PKG_LDFLAGS:=-s -w

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/htokenmeter
  SECTION:=net
  CATEGORY:=Network
  SUBMENU:=AI Gateway
  TITLE:=Self-hosted AI gateway for OpenWrt (OpenAI/Claude/Gemini/Responses)
  URL:=https://github.com/lxp/htokenmeter
  DEPENDS:=$(GO_ARCH_DEPENDS) +ca-bundle
  USERID:=htokenmeter:htokenmeter
endef

define Package/htokenmeter/description
  htokenmeter aggregates OpenAI/Anthropic/Gemini and OpenAI-compatible model
  services behind one LAN endpoint with channel routing, sk- tokens, usage
  and cost metering. SQLite only, single static binary, web console on :8317.
  Targets >=256MB RAM devices (aarch64, x86_64).
endef

define Package/htokenmeter/conffiles
/etc/config/htokenmeter
endef

# Copy the Go module into PKG_BUILD_DIR so go.mod sits at its root
# (the OpenAppFilter src/ copy, adapted to Go module layout).
define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) ./go.mod ./go.sum ./main.go ./common ./controller ./middleware ./model ./relay ./router ./web $(PKG_BUILD_DIR)/
endef

define Package/htokenmeter/install
	$(call GoPackage/Package/Install/Bin,$(1))

	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_BIN) $(CURDIR)/files/htokenmeter.init $(1)/etc/init.d/htokenmeter
	$(INSTALL_CONF) $(CURDIR)/files/htokenmeter.conf $(1)/etc/config/htokenmeter
endef

$(eval $(call GoBinPackage,htokenmeter))
$(eval $(call BuildPackage,htokenmeter))
