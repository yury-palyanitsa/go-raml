import * as path from 'path';
import * as fs from 'fs';
import { writeFile } from 'fs/promises';
import * as crypto from 'crypto';
import {
    workspace,
    ExtensionContext,
    window,
    commands,
    OutputChannel,
    Uri,
    ViewColumn,
    QuickPickItem,
    StatusBarItem,
    StatusBarAlignment,
    WorkspaceFolder,
} from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
    TransportKind,
    State,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;
let outputChannel: OutputChannel;
let rootApiController: RootApiController | undefined;

/**
 * RootApiController owns the per-workspace-folder "root API file" selection.
 *
 * Why this exists: a user editing a trait library (or any non-root RAML
 * fragment) gets diagnostics from its standalone fragment parse, which can
 * miss errors that only surface when the consuming API document is parsed.
 * Pinning a root makes the LSP re-parse that root on every edit, so the open
 * library file inherits the root's context and its diagnostics reflect what
 * the API would actually see.
 *
 * The selection is intentionally NOT persisted. It lives in memory for the
 * lifetime of the extension host and resets on reload — pinning is a
 * working-session intent, not a project setting, and writing it to
 * workspace settings would commit one developer's editing focus to
 * everyone sharing the repo.
 */
class RootApiController {
    private statusBar: StatusBarItem;
    // current[folderUri] is the absolute fs path of the pinned root file (or
    // undefined when no pin is set). In-memory only — discarded on reload.
    private current: Map<string, string | undefined> = new Map();

    constructor(ctx: ExtensionContext) {
        this.statusBar = window.createStatusBarItem(StatusBarAlignment.Right, 50);
        this.statusBar.command = 'ramlLsp.selectRootApi';
        this.statusBar.tooltip = 'Click to select the root API document for this workspace';
        ctx.subscriptions.push(this.statusBar);

        ctx.subscriptions.push(window.onDidChangeActiveTextEditor(() => this.refresh()));
        // When folders change, drop pins for folders that disappeared; the
        // remaining map entries stay valid and don't need re-pushing.
        ctx.subscriptions.push(workspace.onDidChangeWorkspaceFolders(e => {
            for (const removed of e.removed) {
                this.current.delete(removed.uri.toString());
            }
            this.refresh();
        }));
    }

    /** Active workspace folder, derived from the active editor or the first folder. */
    private activeFolder(): WorkspaceFolder | undefined {
        const active = window.activeTextEditor?.document.uri;
        if (active) {
            const folder = workspace.getWorkspaceFolder(active);
            if (folder) {
                return folder;
            }
        }
        return workspace.workspaceFolders?.[0];
    }

    /** Notify the server about the pin change for one workspace folder. */
    private async sendToServer(folder: WorkspaceFolder, rootPath: string | undefined): Promise<void> {
        if (!client) {
            return;
        }
        const workspaceUri = client.code2ProtocolConverter.asUri(folder.uri);
        try {
            if (rootPath) {
                const uri = client.code2ProtocolConverter.asUri(Uri.file(rootPath));
                await client.sendRequest('ramlLsp/setRoot', { workspace: workspaceUri, uri });
            } else {
                await client.sendRequest('ramlLsp/clearRoot', { workspace: workspaceUri });
            }
        } catch (err) {
            outputChannel.appendLine(`Failed to update root API pin for ${folder.name}: ${err}`);
        }
    }

    /** Update the status bar text based on the active editor's folder. */
    private refresh(): void {
        const folder = this.activeFolder();
        if (!folder) {
            this.statusBar.hide();
            return;
        }
        const stored = this.current.get(folder.uri.toString());
        if (stored) {
            const label = path.relative(folder.uri.fsPath, stored).replace(/\\/g, '/') || path.basename(stored);
            this.statusBar.text = `$(file-code) Root API: ${label}`;
        } else {
            this.statusBar.text = `$(file-code) Root API: none`;
        }
        // Only show the status bar item when the active editor is a RAML doc;
        // otherwise it just clutters the bar for unrelated languages.
        const active = window.activeTextEditor?.document;
        if (active && active.languageId === 'raml') {
            this.statusBar.show();
        } else {
            this.statusBar.hide();
        }
    }

    /** Set (or clear) the in-memory pin for a folder and notify the server. */
    async set(folder: WorkspaceFolder, absolutePath: string | undefined): Promise<void> {
        if (absolutePath) {
            const rel = path.relative(folder.uri.fsPath, absolutePath);
            if (rel.startsWith('..') || path.isAbsolute(rel)) {
                window.showErrorMessage('The root API file must be inside the workspace folder.');
                return;
            }
        }
        this.current.set(folder.uri.toString(), absolutePath);
        await this.sendToServer(folder, absolutePath);
        this.refresh();
    }

    /**
     * Re-push every in-memory pin to the server. Used after a server restart
     * so the freshly started process learns the current selection.
     */
    async resendAll(): Promise<void> {
        const folders = workspace.workspaceFolders ?? [];
        for (const folder of folders) {
            const pinned = this.current.get(folder.uri.toString());
            await this.sendToServer(folder, pinned);
        }
    }
}

/** Returns the platform-specific binary filename for raml-lsp. */
function binaryName(): string {
    return process.platform === 'win32' ? 'raml-lsp.exe' : 'raml-lsp';
}

/**
 * Resolves the path to the raml-lsp server binary in priority order:
 *  1. User setting `ramlLsp.serverPath`
 *  2. `<extensionPath>/bin/raml-lsp[.exe]`  (bundled binary)
 *
 * Returns undefined when neither location yields an existing file.
 */
function resolveServerPath(ctx: ExtensionContext): string | undefined {
    const configured = workspace.getConfiguration('ramlLsp').get<string>('serverPath');
    if (configured && configured.trim().length > 0) {
        if (!fs.existsSync(configured)) {
            window.showWarningMessage(
                `ramlLsp.serverPath points to a non-existent file: "${configured}". Falling back to bundled binary.`
            );
        } else {
            return configured;
        }
    }

    const bundled = path.join(ctx.extensionPath, 'bin', binaryName());
    if (fs.existsSync(bundled)) {
        return bundled;
    }

    return undefined;
}

async function startClient(ctx: ExtensionContext): Promise<void> {
    const serverPath = resolveServerPath(ctx);

    if (!serverPath) {
        const buildCmd =
            process.platform === 'win32'
                ? 'go build -o cmd\\raml-lsp-vscode\\bin\\raml-lsp.exe .\\cmd\\raml-lsp\\'
                : 'go build -o cmd/raml-lsp-vscode/bin/raml-lsp ./cmd/raml-lsp/';

        const choice = await window.showErrorMessage(
            `RAML Language Server binary not found. ` +
                `Build it from the repo root with:\n${buildCmd}\n` +
                `Or set "ramlLsp.serverPath" to point to an existing binary.`,
            'Open Settings'
        );
        if (choice === 'Open Settings') {
            await commands.executeCommand('workbench.action.openSettings', 'ramlLsp.serverPath');
        }
        return;
    }

    outputChannel.appendLine(`Starting RAML Language Server: ${serverPath}`);

    const cfg = workspace.getConfiguration('ramlLsp');
    const serverArgs: string[] = [];
    if (cfg.get<boolean>('allowRemote', false)) {
        serverArgs.push('--remote');
    }

    const serverOptions: ServerOptions = {
        run: { command: serverPath, args: serverArgs, transport: TransportKind.stdio },
        debug: { command: serverPath, args: serverArgs, transport: TransportKind.stdio },
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [{ scheme: 'file', language: 'raml' }],
        synchronize: {
            // Re-validate open files when any .raml file on disk changes (e.g. a
            // `uses:` library saved from outside VS Code).
            fileEvents: workspace.createFileSystemWatcher('**/*.raml'),
        },
        outputChannel,
    };

    client = new LanguageClient('ramlLsp', 'RAML Language Server', serverOptions, clientOptions);

    client.onDidChangeState(e => {
        if (e.newState === State.Running) {
            outputChannel.appendLine('RAML Language Server is running.');
        } else if (e.newState === State.Stopped) {
            outputChannel.appendLine('RAML Language Server stopped.');
        }
    });

    ctx.subscriptions.push(client);
    await client.start();
}

export async function activate(ctx: ExtensionContext): Promise<void> {
    outputChannel = window.createOutputChannel('RAML Language Server');
    ctx.subscriptions.push(outputChannel);

    ctx.subscriptions.push(
        commands.registerCommand('ramlLsp.restart', async () => {
            outputChannel.appendLine('Restarting RAML Language Server...');
            if (client) {
                await client.stop();
                client = undefined;
            }
            await startClient(ctx);
            // Re-push any in-memory pin so the restarted server picks it up.
            await rootApiController?.resendAll();
        })
    );

    ctx.subscriptions.push(
        commands.registerTextEditorCommand('ramlLsp.preview', async (textEditor) => {
            if (!client) {
                window.showErrorMessage('RAML Language Server is not running.');
                return;
            }
            const document = textEditor.document;
            const uri = client.code2ProtocolConverter.asUri(document.uri);

            const panel = window.createWebviewPanel(
                'ramlApiPreview',
                `API Console: ${path.basename(document.fileName)}`,
                ViewColumn.Two,
                {
                    enableScripts: true,
                    localResourceRoots: [Uri.file(path.join(ctx.extensionPath, 'assets', 'api-console'))],
                    retainContextWhenHidden: true,
                }
            );

            async function sendModel() {
                const data: { uri: string; model: unknown } = await client!.sendRequest('serialization', {
                    documentIdentifier: { uri },
                });
                panel.webview.postMessage({ content: data.model });
            }

            panel.webview.onDidReceiveMessage(async (event) => {
                if (event.ready === true) {
                    await sendModel();
                }
            }, undefined, ctx.subscriptions);

            const autoReload = workspace.getConfiguration('ramlLsp').get<boolean>('autoReloadPreviewOnSave', true);
            let reloadWatcher = { dispose() {} } as { dispose(): void };
            if (autoReload) {
                reloadWatcher = workspace.onDidSaveTextDocument(async (saved) => {
                    if (!client) { return; }
                    if (saved.fileName === document.fileName) {
                        panel.webview.postMessage({ reload: true });
                        await sendModel();
                    }
                });
            }

            panel.onDidDispose(() => {
                reloadWatcher.dispose();
            }, undefined, ctx.subscriptions);

            const vendorJs = path.join(ctx.extensionPath, 'assets', 'api-console', 'vendor.js');
            const apicJs = path.join(ctx.extensionPath, 'assets', 'api-console', 'apic-build.js');
            const vendorJsUri = panel.webview.asWebviewUri(Uri.file(vendorJs));
            const apicBuildJsUri = panel.webview.asWebviewUri(Uri.file(apicJs));

            const nonce = crypto.randomBytes(16).toString('base64');
            panel.webview.html = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src ${panel.webview.cspSource} 'nonce-${nonce}'; style-src ${panel.webview.cspSource} 'unsafe-inline'; connect-src 'self' http: https:;" />
    <meta name="viewport" content="width=device-width,minimum-scale=1,initial-scale=1,user-scalable=yes">
    <title>API Console</title>
    <style>
      #loader { display: flex; align-items: center; justify-content: center; height: 100vh; }
      .lds-dual-ring { width: 64px; height: 64px; }
      .lds-dual-ring::after { content: " "; display: block; width: 46px; height: 46px; margin: 1px; border-radius: 50%; border: 5px solid currentColor; border-color: currentColor transparent currentColor transparent; animation: lds-dual-ring 1.2s linear infinite; }
      @keyframes lds-dual-ring { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }
    </style>
  </head>
  <body>
    <script src="${vendorJsUri.toString()}"></script>
    <api-console-app app rearrangeEndpoints allowCustom allowCustomBaseUri></api-console-app>
    <div id="loader"><div class="lds-dual-ring"></div></div>
    <script type="module" src="${apicBuildJsUri.toString()}"></script>
    <script nonce="${nonce}">
      (function () {
        const vscode = acquireVsCodeApi();
        window.addEventListener('message', function (e) {
          const apic = document.querySelector('api-console-app');
          if (e.data.reload) {
            document.getElementById('loader').style.display = 'flex';
          }
          if (e.data.content) {
            apic.amf = e.data.content;
            document.getElementById('loader').style.display = 'none';
          }
        });
        vscode.postMessage({ ready: true });
      })();
    </script>
  </body>
</html>`;
        })
    );

    ctx.subscriptions.push(
        commands.registerTextEditorCommand('ramlLsp.serializeToFile', async (textEditor) => {
            if (!client) {
                window.showErrorMessage('RAML Language Server is not running.');
                return;
            }
            const document = textEditor.document;
            const uri = client.code2ProtocolConverter.asUri(document.uri);

            const defaultUri = Uri.file(
                path.join(
                    path.dirname(document.fileName),
                    path.basename(document.fileName, path.extname(document.fileName)) + '.amf.json'
                )
            );

            const saveUri = await window.showSaveDialog({
                defaultUri,
                filters: { 'JSON-LD (AMF Graph)': ['json'], 'All files': ['*'] },
                title: 'Save AMF Graph',
            });
            if (!saveUri) {
                return;
            }

            const data: { uri: string; model: unknown } = await window.withProgress(
                { location: 15 /* ProgressLocation.Notification */, cancellable: false, title: 'Building AMF graph…' },
                () => client!.sendRequest('serialization', { documentIdentifier: { uri } })
            );

            const json = JSON.stringify(data.model, null, 2);
            await writeFile(saveUri.fsPath, json, 'utf8');
            const choice = await window.showInformationMessage(
                `AMF graph saved to ${path.basename(saveUri.fsPath)}`,
                'Open file'
            );
            if (choice === 'Open file') {
                await commands.executeCommand('vscode.open', saveUri);
            }
        })
    );

    ctx.subscriptions.push(
        commands.registerTextEditorCommand('ramlLsp.convert', async (textEditor) => {
            if (!client) {
                window.showErrorMessage('RAML Language Server is not running.');
                return;
            }

            type FormatItem = QuickPickItem & {
                format: string;
                ext: string;
                /** 'none' = no type picker; 'optional' = all-types or single; 'required' = must pick a type */
                typeSelection: 'none' | 'optional' | 'required';
                /** Write document as raw string rather than JSON.stringify. */
                saveAsString?: boolean;
                /** Override save-dialog file-type filters (default: JSON). */
                saveFilters?: { [label: string]: string[] };
            };
            const formatItems: FormatItem[] = [
                {
                    label: '$(file-code) OpenAPI 3.0',
                    description: 'Full API document',
                    format: 'oas3',
                    ext: 'openapi.json',
                    typeSelection: 'none',
                },
                {
                    label: '$(symbol-misc) JSON Schema',
                    description: 'Types / data shapes (Draft 7)',
                    format: 'jsonschema',
                    ext: 'schema.json',
                    typeSelection: 'optional',
                },
                {
                    label: '$(symbol-structure) OAS3 Schema',
                    description: 'Single type as OpenAPI components/schemas snippet',
                    format: 'oas3schema',
                    ext: 'schema.json',
                    typeSelection: 'optional',
                },
                {
                    label: '$(symbol-namespace) RAML 1.0',
                    description: 'Embed JSON Schema into a RAML DataType or Library',
                    format: 'raml',
                    ext: 'raml',
                    typeSelection: 'none',
                    saveAsString: true,
                    saveFilters: { 'RAML 1.0': ['raml'], 'All files': ['*'] },
                },
            ];

            const picked = await window.showQuickPick(formatItems, { placeHolder: 'Select target format' });
            if (!picked) {
                return;
            }

            const document = textEditor.document;
            const uri = client.code2ProtocolConverter.asUri(document.uri);

            // Resolve type name via server-provided list when needed.
            let typeName: string | undefined;
            if (picked.typeSelection !== 'none') {
                let typeNames: string[] = [];
                try {
                    const listResult: { uri: string; types: string[] } = await client.sendRequest(
                        'listTypes',
                        { documentIdentifier: { uri } }
                    );
                    typeNames = listResult.types ?? [];
                } catch {
                    // Non-fatal: fall back to free-text input below.
                }

                if (typeNames.length > 0) {
                    type TypeItem = QuickPickItem & { typeName: string | undefined };
                    const typeItems: TypeItem[] = typeNames.map(n => ({ label: n, typeName: n }));
                    if (picked.typeSelection === 'optional') {
                        typeItems.unshift({ label: '$(list-unordered) All types', description: 'Export every type in the document', typeName: undefined });
                    }
                    const typePicked = await window.showQuickPick(typeItems, { placeHolder: 'Select type to convert' });
                    if (typePicked === undefined) {
                        return;
                    }
                    typeName = typePicked.typeName;
                } else {
                    // Fallback: free-text when list is empty or unavailable.
                    const input = await window.showInputBox({
                        prompt: picked.typeSelection === 'required' ? 'Type name to convert' : 'Type name (leave empty to export all types)',
                        placeHolder: picked.typeSelection === 'required' ? 'MyType' : 'Leave empty for all types',
                    });
                    if (input === undefined) {
                        return;
                    }
                    typeName = input.trim() || undefined;
                }
            }

            let data: { uri: string; document: unknown; typeName?: string };
            try {
                data = await window.withProgress(
                    { location: 15 /* ProgressLocation.Notification */, cancellable: false, title: `Converting to ${picked.label}…` },
                    () => client!.sendRequest('convert', {
                        documentIdentifier: { uri },
                        format: picked.format,
                        ...(typeName !== undefined ? { typeName } : {}),
                    })
                );
            } catch (err: any) {
                window.showErrorMessage(`Conversion failed: ${err?.message ?? err}`);
                return;
            }

            // Use server-returned typeName (may differ by casing), or fall back to the input or file stem.
            const stem = data.typeName ?? typeName ?? path.basename(document.fileName, path.extname(document.fileName));
            const defaultUri = Uri.file(path.join(path.dirname(document.fileName), `${stem}.${picked.ext}`));

            const saveUri = await window.showSaveDialog({
                defaultUri,
                filters: picked.saveFilters ?? { 'JSON': ['json'], 'All files': ['*'] },
                title: `Save as ${picked.label}`,
            });
            if (!saveUri) {
                return;
            }

            const fileContent = picked.saveAsString
                ? String(data.document)
                : JSON.stringify(data.document, null, 2);
            await writeFile(saveUri.fsPath, fileContent, 'utf8');
            const choice = await window.showInformationMessage(
                `Saved to ${path.basename(saveUri.fsPath)}`,
                'Open file'
            );
            if (choice === 'Open file') {
                await commands.executeCommand('vscode.open', saveUri);
            }
        })
    );

    rootApiController = new RootApiController(ctx);

    ctx.subscriptions.push(
        commands.registerCommand('ramlLsp.selectRootApi', async () => {
            const folder = workspace.workspaceFolders?.[0];
            if (!folder) {
                window.showErrorMessage('Open a workspace folder to pin a root API file.');
                return;
            }
            const picked = await window.showOpenDialog({
                canSelectMany: false,
                openLabel: 'Set as Root API',
                filters: { 'RAML files': ['raml'], 'All files': ['*'] },
                defaultUri: folder.uri,
            });
            if (!picked || !picked[0]) {
                return;
            }
            await rootApiController!.set(folder, picked[0].fsPath);
        })
    );

    ctx.subscriptions.push(
        commands.registerTextEditorCommand('ramlLsp.setCurrentAsRootApi', async (textEditor) => {
            const folder = workspace.getWorkspaceFolder(textEditor.document.uri);
            if (!folder) {
                window.showErrorMessage('The active file is not inside a workspace folder.');
                return;
            }
            await rootApiController!.set(folder, textEditor.document.uri.fsPath);
        })
    );

    ctx.subscriptions.push(
        commands.registerCommand('ramlLsp.clearRootApi', async () => {
            const folder = window.activeTextEditor
                ? workspace.getWorkspaceFolder(window.activeTextEditor.document.uri)
                : workspace.workspaceFolders?.[0];
            if (!folder) {
                window.showErrorMessage('No workspace folder to clear the root API for.');
                return;
            }
            await rootApiController!.set(folder, undefined);
        })
    );

    await startClient(ctx);
}

export async function deactivate(): Promise<void> {
    await client?.dispose();
    client = undefined;
}
