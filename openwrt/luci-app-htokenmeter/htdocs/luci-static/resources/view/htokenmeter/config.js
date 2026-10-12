/* htokenmeter base settings: UCI form mapping to /etc/config/htokenmeter. */
'use strict';
'require view';
'require uci';
'require form';

return view.extend({
	render: function () {
		var m, s, o;

		m = new form.Map('htokenmeter', _('htokenmeter - AI Gateway'),
			_('Unified LAN entry for OpenAI/Claude/Gemini model services with usage and cost metering. Manage channels, tokens and prices inside the gateway console.'));

		s = m.section(form.TypedSection, 'htokenmeter');
		s.anonymous = true;

		o = s.option(form.Flag, 'enabled', _('Enabled'));
		o.rmempty = false;
		o.default = '0';

		o = s.option(form.Value, 'port', _('Console port'),
			_('HTTP port of the gateway console and API.'));
		o.datatype = 'port';
		o.default = '8317';

		o = s.option(form.Value, 'db_path', _('SQLite path'),
			_('Empty = /etc/htokenmeter/htokenmeter.db. Tip: point to extroot to spare router flash.'));
		o.placeholder = '/etc/htokenmeter/htokenmeter.db';

		o = s.option(form.Value, 'log_dir', _('Log directory'),
			_('Empty = stderr (visible via logread). Set to write logs to a file instead.'));
		o.placeholder = '';

		o = s.option(form.Value, 'log_retention', _('Log retention (days)'),
			_('Usage logs older than this are removed daily to protect flash. 0 = keep forever.'));
		o.datatype = 'range(0,365)';
		o.default = '30';

		o = s.option(form.Value, 'retry_times', _('Relay retries'),
			_('Retries on 429/5xx before giving up (a different channel tier is tried).'));
		o.datatype = 'range(0,10)';
		o.default = '3';

		o = s.option(form.Value, 'delay', _('Boot delay (seconds)'),
			_('Wait before start when uptime is under 2 minutes.'));
		o.datatype = 'range(0,60)';
		o.default = '5';

		return m.render();
	}
});
