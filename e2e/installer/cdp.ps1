# Minimal sequential DevTools calls, without adding a runtime to the clean guest.
function Connect-DevTools([string]$Address) {
    $uri = [uri]$Address
    if ($uri.Scheme -ne 'ws' -or $uri.Host -ne '127.0.0.1') { throw 'DevTools must use the scratch app loopback endpoint.' }
    $socket = [System.Net.WebSockets.ClientWebSocket]::new()
    $cancel = [System.Threading.CancellationTokenSource]::new(10000)
    try {
        [void]$socket.ConnectAsync($uri, $cancel.Token).GetAwaiter().GetResult()
        return $socket
    } catch {
        $socket.Dispose()
        throw
    } finally { $cancel.Dispose() }
}

function Evaluate-DevTools([System.Net.WebSockets.ClientWebSocket]$Socket, [string]$Expression) {
    $requestId = Get-Random -Minimum 1 -Maximum 2147483647
    $request = @{ id = $requestId; method = 'Runtime.evaluate'; params = @{ expression = $Expression; awaitPromise = $true; returnByValue = $true } } |
        ConvertTo-Json -Depth 8 -Compress
    $cancel = [System.Threading.CancellationTokenSource]::new(60000)
    try {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($request)
        [void]$Socket.SendAsync([System.ArraySegment[byte]]::new($bytes), [System.Net.WebSockets.WebSocketMessageType]::Text, $true, $cancel.Token).GetAwaiter().GetResult()
        $buffer = [byte[]]::new(32768)
        while ($true) {
            $message = [System.IO.MemoryStream]::new()
            try {
                do {
                    $part = $Socket.ReceiveAsync([System.ArraySegment[byte]]::new($buffer), $cancel.Token).GetAwaiter().GetResult()
                    if ($part.MessageType -eq [System.Net.WebSockets.WebSocketMessageType]::Close) { throw 'DevTools closed before replying.' }
                    if ($message.Length + $part.Count -gt 1048576) { throw 'DevTools response exceeded its bound.' }
                    $message.Write($buffer, 0, $part.Count)
                } while (-not $part.EndOfMessage)
                $reply = [System.Text.Encoding]::UTF8.GetString($message.ToArray()) | ConvertFrom-Json
            } finally { $message.Dispose() }
            if ($reply.id -ne $requestId) { continue }
            if ($reply.error) { throw ($reply.error | ConvertTo-Json -Compress) }
            if ($reply.result.exceptionDetails) { throw ($reply.result.exceptionDetails | ConvertTo-Json -Depth 8 -Compress) }
            return $reply.result.result.value
        }
    } finally { $cancel.Dispose() }
}

function Call-DevTools([System.Net.WebSockets.ClientWebSocket]$Socket, [string]$Service, [string]$Method, [object[]]$Arguments = @()) {
    $id = [guid]::NewGuid().ToString()
    $body = @{ object = 0; method = 0; args = @{ 'call-id' = $id; methodName = "lumen/services.$Service.$Method"; args = @($Arguments) } } |
        ConvertTo-Json -Depth 30 -Compress
    # JSON embeds the arguments as data; all service calls run inside the WebView.
    $expression = @"
(async () => {
    const response = await fetch('/wails/runtime', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'x-wails-client-id': 'installer-$id' },
        body: JSON.stringify($body)
    });
    const text = await response.text();
    if (!response.ok) throw new Error(text);
    return text.trim() ? JSON.parse(text) : null;
})()
"@
    return Evaluate-DevTools $Socket $expression
}
