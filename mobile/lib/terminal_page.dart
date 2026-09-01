import 'dart:async';
import 'dart:convert';

import 'package:dartssh2/dartssh2.dart';
import 'package:flutter/material.dart';

import 'ssh_service.dart';

/// 终端页：两种模式。
/// - 主机命令行：输入任意 bocker 子命令（如 container list），流式输出。
/// - 容器终端：PTY 交互式 shell（`bocker container shell <name>`）。
class TerminalPage extends StatefulWidget {
  const TerminalPage({super.key, required this.service, this.containerName});

  final SshService service;

  /// 传入则直接进入该容器的交互终端；否则进入主机命令行模式。
  final String? containerName;

  @override
  State<TerminalPage> createState() => _TerminalPageState();
}

class _TerminalPageState extends State<TerminalPage> {
  SSHSession? _ptySession;
  final _output = StringBuffer();
  final _scroll = ScrollController();
  final _input = TextEditingController();
  final _inputFocus = FocusNode();

  bool _busy = false;
  bool get _isPtyMode => _ptySession != null;
  String? get _ptyContainer => widget.containerName;

  @override
  void initState() {
    super.initState();
    if (_ptyContainer != null) {
      _startPty(_ptyContainer!);
    }
  }

  @override
  void dispose() {
    _ptySession?.close();
    _input.dispose();
    _scroll.dispose();
    _inputFocus.dispose();
    super.dispose();
  }

  void _append(String text) {
    if (!mounted) return;
    setState(() => _output.write(text));
    _autoscroll();
  }

  void _autoscroll() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scroll.hasClients) {
        _scroll.jumpTo(_scroll.position.maxScrollExtent);
      }
    });
  }

  // ---------------------------------------------------------------- PTY 模式

  Future<void> _startPty(String name) async {
    setState(() => _busy = true);
    _append('正在打开容器 $name 的终端…\n');
    try {
      final session = await widget.service.openPtySession([
        'container',
        'shell',
        name,
      ]);
      if (!mounted) {
        session.close();
        return;
      }
      setState(() {
        _ptySession = session;
        _busy = false;
      });
      _append('已连接。输入命令，Ctrl+D 或 exit 退出。\n');
      utf8.decoder.bind(session.stdout).listen(
        (chunk) => _append(chunk),
        onDone: () => _append('\n[会话已结束]\n'),
        onError: (error) => _append('\n[会话错误：$error]\n'),
      );
      unawaited(
        session.done.whenComplete(() {
          if (mounted && _ptySession != null) {
            setState(() => _ptySession = null);
          }
        }),
      );
      _inputFocus.requestFocus();
    } catch (error) {
      if (!mounted) return;
      setState(() => _busy = false);
      _append('打开终端失败：$error\n');
    }
  }

  void _closePty() {
    _ptySession?.close();
    _ptySession = null;
    _append('[已关闭会话]\n');
  }

  void _sendPtyInput(String text) {
    final session = _ptySession;
    if (session == null) return;
    session.stdin.add(utf8.encode(text));
  }

  // ---------------------------------------------------------------- 命令模式

  /// 主机命令行：把输入按空白拆分作为 bocker 参数执行，流式增量输出。
  int _emittedLength = 0;

  Future<void> _runCommand(String raw) {
    final args = raw
        .trim()
        .split(RegExp(r'\s+'))
        .where((part) => part.isNotEmpty)
        .toList();
    if (args.isEmpty) return Future.value();
    _append('bocker ${args.join(' ')}\n');
    setState(() => _busy = true);
    _emittedLength = 0;
    return widget.service.run(args, onOutput: (accumulated) {
      if (accumulated.length > _emittedLength) {
        _append(accumulated.substring(_emittedLength));
        _emittedLength = accumulated.length;
      }
    }).then((result) {
      // 结果可能带 stderr 尾差或 trim 差异，结束时兜底补齐。
      final trimmed = result.output;
      if (trimmed.length > _emittedLength) {
        _append('\n${trimmed.substring(_emittedLength)}');
      }
      _append('\n');
      if (!result.ok && result.exitCode != 0) {
        _append('[退出码 ${result.exitCode}]\n');
      }
      if (mounted) setState(() => _busy = false);
    });
  }

  // ---------------------------------------------------------------- UI

  Widget _buildOutput() {
    return Container(
      width: double.infinity,
      color: const Color(0xFF14181D),
      child: SingleChildScrollView(
        controller: _scroll,
        padding: const EdgeInsets.all(12),
        child: SelectableText(
          _output.toString(),
          style: const TextStyle(
            fontFamily: 'monospace',
            fontSize: 13,
            height: 1.45,
            color: Color(0xFFD7E2EA),
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final isConsole = _ptyContainer == null;
    return Scaffold(
      appBar: AppBar(
        title: Text(isConsole ? '命令行' : '终端 · $_ptyContainer'),
        actions: [
          if (_isPtyMode) ...[
            IconButton(
              tooltip: '发送 Ctrl+C',
              icon: const Icon(Icons.cancel_outlined),
              onPressed: () => _sendPtyInput('\u0003'),
            ),
            IconButton(
              tooltip: '结束会话',
              icon: const Icon(Icons.power_settings_new),
              onPressed: _closePty,
            ),
          ],
          IconButton(
            tooltip: '清屏',
            icon: const Icon(Icons.delete_sweep_outlined),
            onPressed: () => setState(() => _output.clear()),
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: isConsole
                ? _buildConsoleIntro(colors)
                : const SizedBox.shrink(),
          ),
          Expanded(flex: 4, child: _buildOutput()),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _input,
                      focusNode: _inputFocus,
                      enabled: !_busy,
                      textInputAction: TextInputAction.newline,
                      onSubmitted: _submit,
                      decoration: InputDecoration(
                        isDense: true,
                        border: const OutlineInputBorder(),
                        hintText: _isPtyMode ? '输入命令…' : 'bocker …',
                        helperText: isConsole
                            ? '示例：container list --json / version / template list'
                            : null,
                      ),
                      style: const TextStyle(fontFamily: 'monospace'),
                    ),
                  ),
                  const SizedBox(width: 8),
                  FilledButton(
                    onPressed: _busy ? null : () => _submit(_input.text),
                    child: const Icon(Icons.keyboard_return),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildConsoleIntro(ColorScheme colors) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '远程命令行',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 4),
          Text(
            '直接执行服务器上的 bocker 命令，输出实时返回。',
            style: TextStyle(color: colors.onSurfaceVariant, fontSize: 13),
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              for (final sample in const [
                'version',
                'container list',
                'image list',
                'template list',
              ])
                ActionChip(
                  label: Text(sample),
                  onPressed: _busy ? null : () => _submit(sample),
                ),
            ],
          ),
        ],
      ),
    );
  }

  void _submit(String text) {
    if (_isPtyMode) {
      _sendPtyInput('$text\n');
      _input.clear();
      return;
    }
    final command = text.trim();
    if (command.isEmpty) return;
    _input.clear();
    _runCommand(command);
  }
}
