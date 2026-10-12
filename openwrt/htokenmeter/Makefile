# SPDX-License-Identifier: MIT
#
# Copyright (C) 2026 htokenmeter contributors

include $(TOPDIR)/rules.mk

PKG_NAME:=htokenmeter
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

# Build from a release tarball (set by CI) or from the local source tree.
# Local build: symlink this directory into openwrt/package/ and run
#   make package/htokenmeter/compile V=s
# Source fallback points at the same directory as this Makefile.
PKG_SOURCE_URL:=https://github.com/lxp/htokenmeter
PKG_SOURCE_VERSION:=HEAD
PKG_SOURCE_PROTO:=git

PKG_LICENSE:=MIT
PKG_LICENSE_FILES:=LICENSE
PKG_MAINTAINER:=htokenmeter maintainers

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

GO_PKG:=github.com/lxp/htokenmeter
GO_PKG_LDFLAGS_X:=github.com/lxp/htokenmeter/common.Version=$(PKG_VERSION)
GO_PKG_BUILD_PKG:=github.com/lxp/htokenmeter
GO_PKG_LDFLAGS:=-s -w $(GO_PKG_LDFLAGS_X)

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

define Package/htokenmeter/install
	$(call GoPackage/Package/Install/Bin,$(1))

	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_BIN) $(CURDIR)/files/htokenmeter.init $(1)/etc/init.d/htokenmeter
	$(INSTALL_CONF) $(CURDIR)/files/htokenmeter.conf $(1)/etc/config/htokenmeter
endef

$(eval $(call GoBinPackage,htokenmeter))
$(eval $(call BuildPackage,htokenmeter))
