import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dartssh2/dartssh2.dart';

import 'models.dart';

/// SSH 登录凭据。
class SshAuthInfo {
  const SshAuthInfo({
    required this.host,
    required this.port,
    required this.username,
    required this.password,
  });

  final String host;
  final int port;
  final String username;
  final String password;
}

/// 通过 SSH 在远程主机上执行 bocker CLI 的服务。
class SshService {
  SSHClient? _client;
  SshAuthInfo? _auth;
  bool _clientClosed = true;
  String _bockerBinary = 'bocker';

  /// 远程 bocker 版本（登录成功后填充）。
  String bockerVersion = '';

  String? get host => _auth?.host;
  int? get port => _auth?.port;
  String? get username => _auth?.username;
  bool get isConnected => _client != null && !_clientClosed;

  /// 建立 SSH 连接并验证 bocker 可用，成功返回 bocker 版本描述。
  Future<String> connect({
    required String host,
    required int port,
    required String username,
    required String password,
  }) async {
    final auth = SshAuthInfo(
      host: host,
      port: port,
      username: username,
      password: password,
    );
    final client = await _open(auth);
    _client = client;
    _auth = auth;
    try {
      bockerVersion = await _resolveVersion();
    } on SSHError catch (error) {
      disconnect();
      throw Exception('SSH 认证失败：请检查用户名和密码（$error）');
    } catch (_) {
      disconnect();
      rethrow;
    }
    return bockerVersion;
  }

  Future<SSHClient> _open(SshAuthInfo auth) async {
    final SSHSocket socket;
    try {
      socket = await SSHSocket.connect(
        auth.host,
        auth.port,
        timeout: const Duration(seconds: 15),
      );
    } catch (error) {
      throw Exception('无法连接 ${auth.host}:${auth.port}，请检查地址、端口与网络（$error）');
    }
    final client = SSHClient(
      socket,
      username: auth.username,
      onPasswordRequest: () => auth.password,
      handshakeTimeout: const Duration(seconds: 20),
      authTimeout: const Duration(seconds: 20),
    );
    _clientClosed = false;
    unawaited(
      client.done.whenComplete(() {
        _clientClosed = true;
      }),
    );
    return client;
  }

  /// 运行 bocker 子命令。连接断开时会自动尝试重连一次。
  Future<CommandResult> run(
    List<String> args, {
    Duration? timeout,
    void Function(String)? onOutput,
  }) async {
    var attempts = 0;
    while (true) {
      if (_client == null) {
        return const CommandResult(false, '尚未连接服务器', -1);
      }
      if (_clientClosed) {
        if (attempts >= 1) {
          return const CommandResult(
            false,
            '与服务器的连接已断开，请到“设置”页重新连接',
            -1,
          );
        }
        attempts++;
        if (!await _tryReconnect()) {
          return const CommandResult(false, '自动重连服务器失败，请检查网络后重试', -1);
        }
      }
      try {
        return await _executeDirect(args, timeout: timeout, onOutput: onOutput);
      } catch (error) {
        if (attempts >= 1) {
          return CommandResult(false, 'SSH 连接错误：$error', -1);
        }
        attempts++;
        _client?.close();
        _clientClosed = true;
        if (!await _tryReconnect()) {
          return CommandResult(false, 'SSH 连接错误：$error（自动重连失败）', -1);
        }
      }
    }
  }

  Future<CommandResult> _executeDirect(
    List<String> args, {
    Duration? timeout,
    void Function(String)? onOutput,
  }) async {
    final client = _client;
    if (client == null) return const CommandResult(false, '尚未连接服务器', -1);
    final command = _buildCommand(args);
    final session = await client.execute(command);
    final output = StringBuffer();
    final stdoutDone = Completer<void>();
    final stderrDone = Completer<void>();

    void collect(Stream<Uint8List> stream, Completer<void> done) {
      utf8.decoder.bind(stream).listen(
        (chunk) {
          output.write(chunk);
          onOutput?.call(output.toString());
        },
        onDone: done.complete,
        onError: (_) => done.complete(),
      );
    }

    collect(session.stdout, stdoutDone);
    collect(session.stderr, stderrDone);

    Future<int> waitForExit() async {
      await stdoutDone.future;
      await stderrDone.future;
      await session.done;
      return session.exitCode ?? -1;
    }

    final int exitCode;
    try {
      exitCode = timeout == null
          ? await waitForExit()
          : await waitForExit().timeout(timeout);
    } on TimeoutException {
      session.close();
      return CommandResult(false, '命令执行超时：$command', -1);
    }
    return CommandResult(exitCode == 0, output.toString().trim(), exitCode);
  }

  /// 解析远程 bocker 可执行文件位置并取回版本号。
  Future<String> _resolveVersion() async {
    var result = await _executeDirect(
      const ['version'],
      timeout: const Duration(seconds: 20),
    );
    if (result.exitCode == 127) {
      _bockerBinary = '/usr/bin/bocker';
      result = await _executeDirect(
        const ['version'],
        timeout: const Duration(seconds: 20),
      );
    }
    if (!result.ok) {
      throw Exception(
        '服务器上无法运行 bocker：${result.summary.isEmpty ? '未知错误' : result.summary}',
      );
    }
    return result.output.split('\n').first.trim();
  }

  Future<bool> _tryReconnect() async {
    final auth = _auth;
    if (auth == null) return false;
    try {
      _client?.close();
      _client = await _open(auth);
      return true;
    } catch (_) {
      return false;
    }
  }

  String _buildCommand(List<String> args) =>
      [_bockerBinary, ...args].map(_shellQuote).join(' ');

  /// 仅对包含特殊字符的参数做单引号包裹。
  String _shellQuote(String value) {
    if (value.isEmpty) return "''";
    if (RegExp(r'^[A-Za-z0-9_@%+=:,./-]+$').hasMatch(value)) return value;
    return "'${value.replaceAll("'", r"'\''")}'";
  }

  void disconnect() {
    _client?.close();
    _client = null;
    _auth = null;
    _clientClosed = true;
  }
}
