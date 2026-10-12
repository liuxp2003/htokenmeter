/* htokenmeter console view: run-state check + iframe to the gateway console.
 * Follows the luci-app-ddns-go pattern: pidof check, port from UCI,
 * https LuCI cannot iframe http consoles -> open-in-new-tab button. */
'use strict';
'require view';
'require fs';
'require uci';

return view.extend({
	load: function () {
		return uci.load('htokenmeter');
	},

	checkRunning: function () {
		return fs.exec('/bin/pidof', ['htokenmeter']).then(function (res) {
			return res.code === 0;
		}).catch(function () { return false; });
	},

	render: function () {
		var self = this;
		return this.checkRunning().then(function (running) {
			var port = uci.get('htokenmeter', 'config', 'port') || '8317';
			var url = window.location.protocol + '//' + window.location.hostname + ':' + port;
			var container = E('div');

			if (!running) {
				container.appendChild(E('div', { 'class': 'alert-message warning' }, [
					E('p', {}, _('htokenmeter service is not running.')),
					E('p', {}, _('Enable it on the Base Setting page, then return here.'))
				]));
				return container;
			}

			var isHttps = window.location.protocol === 'https:';
			if (isHttps) {
				container.appendChild(E('div', { 'class': 'alert-message info' }, [
					E('p', {}, _('LuCI is served over HTTPS and the gateway console over HTTP; browsers block embedding HTTP frames into HTTPS pages.')),
					E('button', {
						'class': 'btn cbi-button-action',
						'style': 'margin-top:8px',
						'click': function () { window.open(url, '_blank'); }
					}, _('Open htokenmeter console in a new tab'))
				]));
				return container;
			}

			container.appendChild(E('div', { 'style': 'margin-bottom:8px;display:flex;justify-content:space-between' }, [
				E('span', {}, _('Gateway console (default admin password is printed in the system log on first boot: look for "first boot" in logread).')),
				E('button', {
					'class': 'btn cbi-button',
					'click': function () { window.open(url, '_blank'); }
				}, _('Open in new tab'))
			]));
			container.appendChild(E('iframe', {
				'src': url,
				'style': 'width:100%;min-height:760px;border:1px solid #ddd;border-radius:6px;background:#f5f5f5'
			}));
			return container;
		});
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
