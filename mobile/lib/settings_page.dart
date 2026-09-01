import 'package:flutter/material.dart';

import 'ssh_service.dart';
import 'login_page.dart';

class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key, required this.service});

  final SshService service;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  static const appVersion = '1.0.0';

  void _confirmDisconnect() {
    showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('断开连接'),
        content: const Text('将断开当前 SSH 连接并返回登录页。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () {
              Navigator.of(dialogContext).pop();
              widget.service.disconnect();
              if (!mounted) return;
              Navigator.of(context).pushReplacement(
                MaterialPageRoute(builder: (_) => const LoginPage()),
              );
            },
            child: const Text('断开'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final service = widget.service;
    return ListView(
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 24),
      children: [
        _SectionTitle(colors: colors, text: '连接信息'),
        Card(
          child: Column(
            children: [
              ListTile(
                leading: const Icon(Icons.dns_outlined),
                title: const Text('服务器'),
                subtitle: Text('${service.host}:${service.port}'),
              ),
              Divider(height: 1, color: colors.outlineVariant),
              ListTile(
                leading: const Icon(Icons.person_outline),
                title: const Text('用户'),
                subtitle: Text(service.username ?? '-'),
              ),
              Divider(height: 1, color: colors.outlineVariant),
              ListTile(
                leading: const Icon(Icons.terminal_outlined),
                title: const Text('bocker 版本'),
                subtitle: Text(
                  service.bockerVersion.isEmpty ? '-' : service.bockerVersion,
                ),
              ),
              Divider(height: 1, color: colors.outlineVariant),
              ListTile(
                leading: const Icon(Icons.smartphone_outlined),
                title: const Text('客户端版本'),
                subtitle: const Text('Bocker Mobile v$appVersion'),
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        _SectionTitle(colors: colors, text: '操作'),
        Card(
          child: ListTile(
            leading: Icon(Icons.logout, color: colors.error),
            title: Text('断开连接', style: TextStyle(color: colors.error)),
            subtitle: Text(
              '断开 SSH 并返回登录页',
              style: TextStyle(color: colors.onSurfaceVariant, fontSize: 12),
            ),
            onTap: _confirmDisconnect,
          ),
        ),
        const SizedBox(height: 16),
        _SectionTitle(colors: colors, text: '说明'),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Text(
              '本应用通过 SSH 登录远程主机并执行 bocker 命令行工具，'
              '实现容器、镜像与模板的远程管理。\n'
              '要求：服务器已安装 bocker（deb 包或手动安装），'
              '且当前用户可直接运行 bocker（日常操作无需 sudo）。'
              '若登录页勾选了“记住密码”，密码将以明文保存在本机。',
              style: TextStyle(color: colors.onSurfaceVariant, height: 1.6),
            ),
          ),
        ),
      ],
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle({required this.colors, required this.text});

  final ColorScheme colors;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(left: 4, bottom: 8),
      child: Text(
        text,
        style: TextStyle(
          color: colors.onSurfaceVariant,
          fontWeight: FontWeight.w600,
          fontSize: 13,
        ),
      ),
    );
  }
}
