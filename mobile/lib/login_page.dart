import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'home_page.dart';
import 'ssh_service.dart';

class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  static const _keyHost = 'bocker.host';
  static const _keyPort = 'bocker.port';
  static const _keyUser = 'bocker.user';
  static const _keyRemember = 'bocker.remember';
  static const _keyPassword = 'bocker.password';

  final _formKey = GlobalKey<FormState>();
  final _host = TextEditingController();
  final _port = TextEditingController(text: '22');
  final _user = TextEditingController();
  final _password = TextEditingController();
  bool _obscure = true;
  bool _remember = false;
  bool _connecting = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _restore();
  }

  Future<void> _restore() async {
    final prefs = await SharedPreferences.getInstance();
    if (!mounted) return;
    setState(() {
      _host.text = prefs.getString(_keyHost) ?? '';
      _user.text = prefs.getString(_keyUser) ?? '';
      final port = prefs.getInt(_keyPort);
      if (port != null && port > 0) _port.text = port.toString();
      _remember = prefs.getBool(_keyRemember) ?? false;
      if (_remember) {
        final saved = prefs.getString(_keyPassword);
        if (saved != null && saved.isNotEmpty) _password.text = saved;
      }
    });
  }

  Future<void> _save() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyHost, _host.text.trim());
    await prefs.setString(_keyUser, _user.text.trim());
    await prefs.setInt(_keyPort, _portNumber);
    await prefs.setBool(_keyRemember, _remember);
    if (_remember) {
      await prefs.setString(_keyPassword, _password.text);
    } else {
      await prefs.remove(_keyPassword);
    }
  }

  int get _portNumber => int.tryParse(_port.text.trim()) ?? 22;

  /// 支持在地址栏直接输入 host:port 或 [ipv6]:port。
  (String, int) get _endpoint {
    var host = _host.text.trim();
    var port = _portNumber;
    final bracketed = RegExp(r'^\[(.+)\](?::(\d+))?$').firstMatch(host);
    if (bracketed != null) {
      host = bracketed.group(1)!;
      final parsed = int.tryParse(bracketed.group(2) ?? '');
      if (parsed != null) port = parsed;
      return (host, port);
    }
    if (host.contains(':')) {
      final index = host.lastIndexOf(':');
      final maybePort = int.tryParse(host.substring(index + 1));
      if (maybePort != null && host.substring(0, index).isNotEmpty) {
        return (host.substring(0, index), maybePort);
      }
    }
    return (host, port);
  }

  Future<void> _connect() async {
    if (!_formKey.currentState!.validate()) return;
    final (host, port) = _endpoint;
    FocusScope.of(context).unfocus();
    setState(() {
      _connecting = true;
      _error = null;
    });
    final service = SshService();
    try {
      await service.connect(
        host: host,
        port: port,
        username: _user.text.trim(),
        password: _password.text,
      );
      await _save();
      if (!mounted) return;
      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => HomePage(service: service)),
      );
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _connecting = false;
        _error = error.toString().replaceFirst(RegExp(r'^Exception:\s*'), '');
      });
    }
  }

  @override
  void dispose() {
    _host.dispose();
    _port.dispose();
    _user.dispose();
    _password.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Form(
                key: _formKey,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Icon(Icons.view_in_ar, size: 56, color: colors.primary),
                    const SizedBox(height: 12),
                    Text(
                      'Bocker 移动管理',
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.headlineSmall,
                    ),
                    const SizedBox(height: 6),
                    Text(
                      '通过 SSH 连接 Bocker 主机，管理容器、镜像与模板',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: colors.onSurfaceVariant),
                    ),
                    const SizedBox(height: 28),
                    TextFormField(
                      controller: _host,
                      enabled: !_connecting,
                      keyboardType: TextInputType.url,
                      textInputAction: TextInputAction.next,
                      decoration: const InputDecoration(
                        labelText: '服务器地址（IP 或域名）',
                        hintText: '例如 192.168.1.10 或 my.bocker.dev',
                        prefixIcon: Icon(Icons.dns_outlined),
                      ),
                      validator: (value) {
                        final text = value?.trim() ?? '';
                        if (text.isEmpty) return '请输入服务器地址';
                        if (RegExp(r'\s').hasMatch(text)) return '地址中不能包含空格';
                        return null;
                      },
                    ),
                    const SizedBox(height: 16),
                    TextFormField(
                      controller: _port,
                      enabled: !_connecting,
                      keyboardType: TextInputType.number,
                      textInputAction: TextInputAction.next,
                      decoration: const InputDecoration(
                        labelText: 'SSH 端口',
                        prefixIcon: Icon(Icons.numbers_outlined),
                      ),
                      validator: (value) {
                        final port = int.tryParse(value?.trim() ?? '');
                        if (port == null || port < 1 || port > 65535) {
                          return '端口需为 1-65535';
                        }
                        return null;
                      },
                    ),
                    const SizedBox(height: 16),
                    TextFormField(
                      controller: _user,
                      enabled: !_connecting,
                      textInputAction: TextInputAction.next,
                      decoration: const InputDecoration(
                        labelText: '用户名',
                        prefixIcon: Icon(Icons.person_outline),
                      ),
                      validator: (value) =>
                          (value?.trim() ?? '').isEmpty ? '请输入用户名' : null,
                    ),
                    const SizedBox(height: 16),
                    TextFormField(
                      controller: _password,
                      enabled: !_connecting,
                      obscureText: _obscure,
                      onFieldSubmitted: (_) => _connect(),
                      decoration: InputDecoration(
                        labelText: '密码',
                        prefixIcon: const Icon(Icons.password_outlined),
                        suffixIcon: IconButton(
                          icon: Icon(
                            _obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined,
                          ),
                          onPressed: () => setState(() => _obscure = !_obscure),
                        ),
                      ),
                      validator: (value) =>
                          (value ?? '').isEmpty ? '请输入密码' : null,
                    ),
                    SwitchListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('记住密码'),
                      subtitle: Text(
                        '密码将以明文保存在本机',
                        style: TextStyle(
                          fontSize: 12,
                          color: colors.onSurfaceVariant,
                        ),
                      ),
                      value: _remember,
                      onChanged: _connecting
                          ? null
                          : (value) => setState(() => _remember = value),
                    ),
                    if (_error != null) ...[
                      const SizedBox(height: 8),
                      Container(
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: colors.errorContainer,
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Text(
                          _error!,
                          style: TextStyle(
                            color: colors.onErrorContainer,
                            fontSize: 13,
                          ),
                        ),
                      ),
                    ],
                    const SizedBox(height: 20),
                    FilledButton(
                      onPressed: _connecting ? null : _connect,
                      style: FilledButton.styleFrom(
                        padding: const EdgeInsets.symmetric(vertical: 14),
                      ),
                      child: _connecting
                          ? const SizedBox(
                              width: 20,
                              height: 20,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Text('连接'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}