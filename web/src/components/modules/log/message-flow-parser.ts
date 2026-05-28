// [fork] Front-end-only parser for log detail AI message flow visualization.
import type { LogVisualSourceMode } from './detail-store';

export type MessageFlowProtocol = 'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'unsupported';
export type MessageFlowRole = 'system' | 'user' | 'assistant' | 'tool' | 'other';
export type MessageFlowItemSource = 'request' | 'response';

export interface MessageFlowTool {
    id: string;
    type: string;
    name: string;
    description?: string;
    parameters?: unknown;
    raw: unknown;
}

export interface MessageFlowToolCall {
    id?: string;
    name: string;
    arguments?: string;
    raw?: unknown;
}

export interface MessageFlowItem {
    id: string;
    role: MessageFlowRole;
    originalRole?: string;
    source: MessageFlowItemSource;
    protocol: MessageFlowProtocol;
    title: string;
    content?: string;
    reasoning?: string;
    toolCalls?: MessageFlowToolCall[];
    tools?: MessageFlowTool[];
    raw?: unknown;
}

export interface MessageFlowParseResult {
    sourceMode: LogVisualSourceMode;
    protocolPair: {
        request: MessageFlowProtocol;
        response: MessageFlowProtocol;
    };
    items: MessageFlowItem[];
    tools: MessageFlowTool[];
    warnings: string[];
    error?: string;
}

export interface ParseLogMessageFlowInput {
    sourceMode: LogVisualSourceMode;
    requestProtocol?: string;
    responseProtocol?: string;
    requestContent?: string;
    responseContent?: string;
    responseFallbackContent?: string;
}

interface SseEvent {
    event?: string;
    data: string;
}

type JsonObject = Record<string, unknown>;

function isRecord(value: unknown): value is JsonObject {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function asArray(value: unknown): unknown[] {
    return Array.isArray(value) ? value : [];
}

function asString(value: unknown): string | undefined {
    if (typeof value === 'string') return value;
    return undefined;
}

function normalizeRole(role: unknown): MessageFlowRole {
    const raw = asString(role)?.toLowerCase();
    if (raw === 'system' || raw === 'developer') return 'system';
    if (raw === 'user') return 'user';
    if (raw === 'assistant') return 'assistant';
    if (raw === 'tool' || raw === 'function') return 'tool';
    return 'other';
}

function normalizeProtocolName(protocol?: string): MessageFlowProtocol {
    const normalized = protocol?.trim().toLowerCase().replace(/[_-]+/g, ' ');
    if (!normalized) return 'unsupported';
    if (normalized.includes('openai') && (normalized.includes('chat') || normalized.includes('completion'))) {
        return 'openai_chat';
    }
    if (normalized.includes('openai') && normalized.includes('response')) {
        return 'openai_responses';
    }
    if (normalized.includes('anthropic') && normalized.includes('message')) {
        return 'anthropic_messages';
    }
    return 'unsupported';
}

export function normalizeLogProtocol(protocol?: string): string {
    const parsed = normalizeProtocolName(protocol);
    if (parsed !== 'unsupported') return parsed;
    return protocol?.trim().toLowerCase().replace(/\s+/g, ' ') ?? '';
}

export function detectMessageFlowProtocol(content: string | undefined, kind: MessageFlowItemSource, protocolHint?: string): MessageFlowProtocol {
    const hinted = normalizeProtocolName(protocolHint);
    if (hinted !== 'unsupported') return hinted;

    if (kind === 'request') {
        return inferProtocolFromRequestPayload(parseJson(content));
    }

    const events = parseSse(content);
    if (events.length > 0) return inferProtocolFromSse(events);
    return inferProtocolFromResponsePayload(parseJson(content));
}

function parseJson(content?: string): unknown | undefined {
    const trimmed = content?.trim();
    if (!trimmed) return undefined;
    try {
        return JSON.parse(trimmed);
    } catch {
        return undefined;
    }
}

function parseSse(content?: string): SseEvent[] {
    const text = content?.trim();
    if (!text) return [];
    const events: SseEvent[] = [];
    const normalized = text.replace(/\r\n/g, '\n');
    const blocks = normalized.split(/\n\n+/);

    for (const block of blocks) {
        const lines = block.split('\n');
        const dataLines: string[] = [];
        let eventName: string | undefined;

        for (const line of lines) {
            if (line.startsWith('event:')) {
                eventName = line.slice(6).trim();
            } else if (line.startsWith('data:')) {
                dataLines.push(line.slice(5).trimStart());
            }
        }

        if (dataLines.length > 0) {
            events.push({ event: eventName, data: dataLines.join('\n') });
        }
    }

    return events;
}

function contentToText(content: unknown): string {
    if (typeof content === 'string') return content;
    if (content === null || typeof content === 'undefined') return '';

    if (Array.isArray(content)) {
        const parts = content.map((part) => {
            if (typeof part === 'string') return part;
            if (!isRecord(part)) return JSON.stringify(part);

            const type = asString(part.type);
            if (typeof part.text === 'string') return part.text;
            if (typeof part.thinking === 'string') return part.thinking;
            if (typeof part.partial_json === 'string') return part.partial_json;
            if (type === 'image_url' || type === 'input_image') return '[image omitted]';
            if (type === 'input_audio' || type === 'audio') return '[audio omitted]';
            if (type === 'tool_use') return `[tool_use:${asString(part.name) ?? 'unknown'}]`;
            if (type === 'tool_result') return contentToText(part.content);
            return JSON.stringify(part);
        });
        return parts.filter(Boolean).join('\n');
    }

    if (isRecord(content)) {
        if (typeof content.text === 'string') return content.text;
        if (Array.isArray(content.items)) return contentToText(content.items);
        if (Array.isArray(content.content)) return contentToText(content.content);
    }

    return JSON.stringify(content, null, 2);
}

function stringifyJson(value: unknown): string {
    if (typeof value === 'string') return value;
    try {
        return JSON.stringify(value, null, 2);
    } catch {
        return String(value);
    }
}

function toolNameFromRaw(raw: JsonObject): string {
    const functionObj = isRecord(raw.function) ? raw.function : undefined;
    const name = asString(raw.name) ?? asString(functionObj?.name);
    return name ?? asString(raw.type) ?? 'tool';
}

function normalizeTool(rawTool: unknown, index: number): MessageFlowTool {
    if (!isRecord(rawTool)) {
        return {
            id: `tool-${index}`,
            type: 'unknown',
            name: `tool-${index + 1}`,
            raw: rawTool,
        };
    }

    const fn = isRecord(rawTool.function) ? rawTool.function : undefined;
    return {
        id: asString(rawTool.id) ?? asString(rawTool.name) ?? asString(fn?.name) ?? `tool-${index}`,
        type: asString(rawTool.type) ?? 'function',
        name: toolNameFromRaw(rawTool),
        description: asString(rawTool.description) ?? asString(fn?.description),
        parameters: rawTool.parameters ?? fn?.parameters ?? rawTool.input_schema,
        raw: rawTool,
    };
}

function normalizeFunctionTool(rawFunction: unknown, index: number): MessageFlowTool {
    if (!isRecord(rawFunction)) {
        return normalizeTool(rawFunction, index);
    }
    return {
        id: asString(rawFunction.name) ?? `function-${index}`,
        type: 'function',
        name: asString(rawFunction.name) ?? `function-${index + 1}`,
        description: asString(rawFunction.description),
        parameters: rawFunction.parameters,
        raw: rawFunction,
    };
}

function extractChatTools(payload: JsonObject): MessageFlowTool[] {
    const tools = asArray(payload.tools).map(normalizeTool);
    const functions = asArray(payload.functions).map(normalizeFunctionTool);
    return [...tools, ...functions];
}

function extractResponsesTools(payload: JsonObject): MessageFlowTool[] {
    return asArray(payload.tools).map(normalizeTool);
}

function extractAnthropicTools(payload: JsonObject): MessageFlowTool[] {
    return asArray(payload.tools).map(normalizeTool);
}

function normalizeToolCall(rawCall: unknown, index: number): MessageFlowToolCall {
    if (!isRecord(rawCall)) {
        return { name: `tool_call_${index + 1}`, raw: rawCall };
    }

    const fn = isRecord(rawCall.function) ? rawCall.function : undefined;
    return {
        id: asString(rawCall.id) ?? asString(rawCall.call_id),
        name: asString(rawCall.name) ?? asString(fn?.name) ?? `tool_call_${index + 1}`,
        arguments: asString(rawCall.arguments) ?? asString(fn?.arguments) ?? stringifyJson(rawCall.input),
        raw: rawCall,
    };
}

function makeItem(params: Omit<MessageFlowItem, 'id'> & { id: string }): MessageFlowItem {
    return params;
}

function titleForRole(role: MessageFlowRole, source: MessageFlowItemSource): string {
    if (source === 'response') return role === 'assistant' ? 'assistant response' : `${role} response`;
    return role;
}

function inferProtocolFromRequestPayload(payload: unknown): MessageFlowProtocol {
    if (!isRecord(payload)) return 'unsupported';
    if (Array.isArray(payload.messages)) {
        if ('max_tokens' in payload && ('system' in payload || Array.isArray(payload.tools))) {
            return 'anthropic_messages';
        }
        return 'openai_chat';
    }
    if ('input' in payload || 'instructions' in payload) return 'openai_responses';
    return 'unsupported';
}

function inferProtocolFromResponsePayload(payload: unknown): MessageFlowProtocol {
    if (!isRecord(payload)) return 'unsupported';
    if (payload.object === 'response' || Array.isArray(payload.output)) return 'openai_responses';
    if (payload.object === 'chat.completion' || payload.object === 'chat.completion.chunk' || Array.isArray(payload.choices)) return 'openai_chat';
    if (payload.type === 'message' || Array.isArray(payload.content)) return 'anthropic_messages';
    return 'unsupported';
}

function inferProtocolFromSse(events: SseEvent[]): MessageFlowProtocol {
    for (const event of events) {
        if (event.data === '[DONE]') continue;
        const parsed = parseJson(event.data);
        if (!isRecord(parsed)) continue;
        const type = asString(parsed.type) ?? event.event;
        if (type?.startsWith('response.')) return 'openai_responses';
        if (type?.startsWith('message_') || type?.startsWith('content_block_')) return 'anthropic_messages';
        if (parsed.object === 'chat.completion.chunk' || Array.isArray(parsed.choices)) return 'openai_chat';
    }
    return 'unsupported';
}

function parseOpenAIChatRequest(payload: JsonObject, protocol: MessageFlowProtocol, tools: MessageFlowTool[]): MessageFlowItem[] {
    return asArray(payload.messages).map((message, index) => {
        const msg = isRecord(message) ? message : {};
        const role = normalizeRole(msg.role);
        const toolCalls = asArray(msg.tool_calls).map(normalizeToolCall);
        if (isRecord(msg.function_call)) {
            toolCalls.push(normalizeToolCall(msg.function_call, toolCalls.length));
        }

        return makeItem({
            id: `request-chat-${index}`,
            role,
            originalRole: asString(msg.role),
            source: 'request',
            protocol,
            title: titleForRole(role, 'request'),
            content: contentToText(msg.content),
            reasoning: asString(msg.reasoning_content) ?? asString(msg.reasoning),
            toolCalls,
            tools,
            raw: message,
        });
    });
}

function parseResponsesInputItem(item: unknown, index: number, protocol: MessageFlowProtocol, tools: MessageFlowTool[]): MessageFlowItem | null {
    if (typeof item === 'string') {
        return makeItem({
            id: `request-responses-input-${index}`,
            role: 'user',
            source: 'request',
            protocol,
            title: 'user',
            content: item,
            tools,
            raw: item,
        });
    }
    if (!isRecord(item)) return null;

    const type = asString(item.type);
    if (type === 'function_call') {
        return makeItem({
            id: `request-responses-tool-call-${index}`,
            role: 'assistant',
            source: 'request',
            protocol,
            title: 'assistant tool call',
            toolCalls: [normalizeToolCall(item, 0)],
            tools,
            raw: item,
        });
    }
    if (type === 'function_call_output') {
        return makeItem({
            id: `request-responses-tool-output-${index}`,
            role: 'tool',
            source: 'request',
            protocol,
            title: 'tool output',
            content: contentToText(item.output),
            tools,
            raw: item,
        });
    }
    if (type === 'reasoning') {
        return makeItem({
            id: `request-responses-reasoning-${index}`,
            role: 'assistant',
            source: 'request',
            protocol,
            title: 'assistant reasoning',
            reasoning: extractResponsesReasoning(item),
            tools,
            raw: item,
        });
    }

    const role = normalizeRole(item.role || (type === 'input_text' || type === 'input_image' ? 'user' : undefined));
    return makeItem({
        id: `request-responses-${index}`,
        role,
        originalRole: asString(item.role),
        source: 'request',
        protocol,
        title: titleForRole(role, 'request'),
        content: contentToText(item.content ?? item.text ?? item),
        tools,
        raw: item,
    });
}

function parseOpenAIResponsesRequest(payload: JsonObject, protocol: MessageFlowProtocol, tools: MessageFlowTool[]): MessageFlowItem[] {
    const items: MessageFlowItem[] = [];
    if (typeof payload.instructions === 'string' && payload.instructions.trim()) {
        items.push(makeItem({
            id: 'request-responses-instructions',
            role: 'system',
            source: 'request',
            protocol,
            title: 'system',
            content: payload.instructions,
            tools,
            raw: { instructions: payload.instructions },
        }));
    }

    if (typeof payload.input === 'string') {
        items.push(makeItem({
            id: 'request-responses-input-text',
            role: 'user',
            source: 'request',
            protocol,
            title: 'user',
            content: payload.input,
            tools,
            raw: payload.input,
        }));
        return items;
    }

    for (const [index, inputItem] of asArray(payload.input).entries()) {
        const parsed = parseResponsesInputItem(inputItem, index, protocol, tools);
        if (parsed) items.push(parsed);
    }

    return items;
}

function parseAnthropicSystem(system: unknown, protocol: MessageFlowProtocol, tools: MessageFlowTool[]): MessageFlowItem | null {
    const content = contentToText(system);
    if (!content.trim()) return null;
    return makeItem({
        id: 'request-anthropic-system',
        role: 'system',
        source: 'request',
        protocol,
        title: 'system',
        content,
        tools,
        raw: system,
    });
}

function parseAnthropicRequest(payload: JsonObject, protocol: MessageFlowProtocol, tools: MessageFlowTool[]): MessageFlowItem[] {
    const items: MessageFlowItem[] = [];
    const systemItem = parseAnthropicSystem(payload.system, protocol, tools);
    if (systemItem) items.push(systemItem);

    for (const [index, message] of asArray(payload.messages).entries()) {
        const msg = isRecord(message) ? message : {};
        const role = normalizeRole(msg.role);
        const contentBlocks = Array.isArray(msg.content) ? msg.content : [];
        const toolCalls = contentBlocks
            .filter((block) => isRecord(block) && block.type === 'tool_use')
            .map(normalizeToolCall);

        items.push(makeItem({
            id: `request-anthropic-${index}`,
            role,
            originalRole: asString(msg.role),
            source: 'request',
            protocol,
            title: titleForRole(role, 'request'),
            content: contentToText(msg.content),
            toolCalls,
            tools,
            raw: message,
        }));
    }

    return items;
}

function parseRequestPayload(content: string | undefined, protocolHint?: string): { protocol: MessageFlowProtocol; items: MessageFlowItem[]; tools: MessageFlowTool[]; warnings: string[]; error?: string } {
    const warnings: string[] = [];
    const payload = parseJson(content);
    if (!payload) {
        return { protocol: normalizeProtocolName(protocolHint), items: [], tools: [], warnings, error: 'invalid_request_json' };
    }

    const protocol = normalizeProtocolName(protocolHint) !== 'unsupported'
        ? normalizeProtocolName(protocolHint)
        : inferProtocolFromRequestPayload(payload);

    if (!isRecord(payload)) {
        return { protocol, items: [], tools: [], warnings, error: 'invalid_request_shape' };
    }

    switch (protocol) {
        case 'openai_chat': {
            const tools = extractChatTools(payload);
            return { protocol, tools, warnings, items: parseOpenAIChatRequest(payload, protocol, tools) };
        }
        case 'openai_responses': {
            const tools = extractResponsesTools(payload);
            return { protocol, tools, warnings, items: parseOpenAIResponsesRequest(payload, protocol, tools) };
        }
        case 'anthropic_messages': {
            const tools = extractAnthropicTools(payload);
            return { protocol, tools, warnings, items: parseAnthropicRequest(payload, protocol, tools) };
        }
        default:
            return { protocol, items: [], tools: [], warnings, error: 'unsupported_protocol' };
    }
}

function extractResponsesReasoning(item: JsonObject): string | undefined {
    const summary = asArray(item.summary)
        .map((part) => isRecord(part) ? asString(part.text) : undefined)
        .filter(Boolean)
        .join('\n');
    return summary || asString(item.reasoning) || asString(item.reasoning_content);
}

function parseOpenAIChatResponseObject(payload: JsonObject, protocol: MessageFlowProtocol): MessageFlowItem[] {
    const items: MessageFlowItem[] = [];
    for (const [index, choice] of asArray(payload.choices).entries()) {
        const choiceObj = isRecord(choice) ? choice : {};
        const message = isRecord(choiceObj.message) ? choiceObj.message : (isRecord(choiceObj.delta) ? choiceObj.delta : {});
        const role = normalizeRole(message.role || 'assistant');
        const toolCalls = asArray(message.tool_calls).map(normalizeToolCall);
        if (isRecord(message.function_call)) {
            toolCalls.push(normalizeToolCall(message.function_call, toolCalls.length));
        }
        const reasoning = asString(message.reasoning_content) ?? asString(message.reasoning);
        const content = contentToText(message.content);

        if (content || reasoning || toolCalls.length > 0) {
            items.push(makeItem({
                id: `response-chat-${index}`,
                role,
                originalRole: asString(message.role),
                source: 'response',
                protocol,
                title: titleForRole(role, 'response'),
                content,
                reasoning,
                toolCalls,
                raw: choice,
            }));
        }
    }
    return items;
}

function mergeChatStream(events: SseEvent[], protocol: MessageFlowProtocol): { items: MessageFlowItem[]; warnings: string[] } {
    const warnings: string[] = [];
    const contentByIndex = new Map<number, string>();
    const reasoningByIndex = new Map<number, string>();
    const toolCallsByIndex = new Map<number, Map<number, MessageFlowToolCall>>();
    let rawCount = 0;

    for (const event of events) {
        if (event.data === '[DONE]') continue;
        const payload = parseJson(event.data);
        if (!isRecord(payload)) {
            warnings.push('invalid_sse_chunk');
            continue;
        }
        rawCount++;
        for (const choice of asArray(payload.choices)) {
            const choiceObj = isRecord(choice) ? choice : {};
            const index = typeof choiceObj.index === 'number' ? choiceObj.index : 0;
            const delta = isRecord(choiceObj.delta) ? choiceObj.delta : {};
            const content = contentToText(delta.content);
            if (content) contentByIndex.set(index, `${contentByIndex.get(index) ?? ''}${content}`);
            const reasoning = asString(delta.reasoning_content) ?? asString(delta.reasoning);
            if (reasoning) reasoningByIndex.set(index, `${reasoningByIndex.get(index) ?? ''}${reasoning}`);
            for (const toolCallRaw of asArray(delta.tool_calls)) {
                if (!isRecord(toolCallRaw)) continue;
                const toolIndex = typeof toolCallRaw.index === 'number' ? toolCallRaw.index : 0;
                const existingByToolIndex = toolCallsByIndex.get(index) ?? new Map<number, MessageFlowToolCall>();
                const existing = existingByToolIndex.get(toolIndex) ?? { name: '', arguments: '' };
                const next = normalizeToolCall(toolCallRaw, toolIndex);
                existing.id = next.id ?? existing.id;
                existing.name = `${existing.name ?? ''}${next.name === `tool_call_${toolIndex + 1}` ? '' : next.name}`;
                existing.arguments = `${existing.arguments ?? ''}${next.arguments ?? ''}`;
                existing.raw = toolCallRaw;
                existingByToolIndex.set(toolIndex, existing);
                toolCallsByIndex.set(index, existingByToolIndex);
            }
        }
    }

    const indexes = new Set<number>([
        ...contentByIndex.keys(),
        ...reasoningByIndex.keys(),
        ...toolCallsByIndex.keys(),
    ]);
    const items = [...indexes].sort((a, b) => a - b).map((index) => makeItem({
        id: `response-chat-stream-${index}`,
        role: 'assistant',
        source: 'response',
        protocol,
        title: 'assistant response',
        content: contentByIndex.get(index),
        reasoning: reasoningByIndex.get(index),
        toolCalls: [...(toolCallsByIndex.get(index)?.values() ?? [])].map((call, callIndex) => ({
            ...call,
            name: call.name || `tool_call_${callIndex + 1}`,
        })),
        raw: { chunks: rawCount },
    }));

    return { items, warnings };
}

function parseResponsesOutputItem(item: unknown, index: number, protocol: MessageFlowProtocol): MessageFlowItem | null {
    if (!isRecord(item)) return null;
    const type = asString(item.type);
    if (type === 'reasoning') {
        return makeItem({
            id: `response-responses-reasoning-${index}`,
            role: 'assistant',
            source: 'response',
            protocol,
            title: 'assistant response',
            reasoning: extractResponsesReasoning(item),
            raw: item,
        });
    }
    if (type === 'function_call') {
        return makeItem({
            id: `response-responses-tool-call-${index}`,
            role: 'assistant',
            source: 'response',
            protocol,
            title: 'assistant response',
            toolCalls: [normalizeToolCall(item, 0)],
            raw: item,
        });
    }
    if (type === 'message') {
        return makeItem({
            id: `response-responses-message-${index}`,
            role: normalizeRole(item.role || 'assistant'),
            originalRole: asString(item.role),
            source: 'response',
            protocol,
            title: 'assistant response',
            content: contentToText(item.content),
            raw: item,
        });
    }
    return null;
}

function combineAssistantResponseItems(items: MessageFlowItem[], protocol: MessageFlowProtocol, raw: unknown): MessageFlowItem[] {
    const reasoning = items.map((item) => item.reasoning).filter(Boolean).join('\n');
    const content = items.map((item) => item.content).filter(Boolean).join('\n');
    const toolCalls = items.flatMap((item) => item.toolCalls ?? []);
    if (!reasoning && !content && toolCalls.length === 0) return [];
    return [makeItem({
        id: 'response-assistant-combined',
        role: 'assistant',
        source: 'response',
        protocol,
        title: 'assistant response',
        reasoning,
        content,
        toolCalls,
        raw,
    })];
}

function parseOpenAIResponsesResponseObject(payload: JsonObject, protocol: MessageFlowProtocol): MessageFlowItem[] {
    const pieces = asArray(payload.output)
        .map((item, index) => parseResponsesOutputItem(item, index, protocol))
        .filter((item): item is MessageFlowItem => item !== null);
    return combineAssistantResponseItems(pieces, protocol, payload);
}

function mergeResponsesStream(events: SseEvent[], protocol: MessageFlowProtocol): { items: MessageFlowItem[]; warnings: string[] } {
    const warnings: string[] = [];
    let content = '';
    let reasoning = '';
    const toolCalls = new Map<string, MessageFlowToolCall>();
    let completedResponse: JsonObject | undefined;

    for (const event of events) {
        const payload = parseJson(event.data);
        if (!isRecord(payload)) {
            if (event.data !== '[DONE]') warnings.push('invalid_sse_chunk');
            continue;
        }
        const type = asString(payload.type) ?? event.event;
        if (type === 'response.completed' && isRecord(payload.response)) {
            completedResponse = payload.response;
        }
        if (type === 'response.output_text.delta') {
            content += asString(payload.delta) ?? asString(payload.text) ?? '';
        }
        if (type === 'response.output_text.done') {
            const text = asString(payload.text);
            if (text && !content.includes(text)) content += text;
        }
        if (type === 'response.reasoning_summary_text.delta') {
            reasoning += asString(payload.delta) ?? asString(payload.text) ?? '';
        }
        if (type === 'response.reasoning_summary_text.done') {
            const text = asString(payload.text);
            if (text && !reasoning.includes(text)) reasoning += text;
        }
        if (type === 'response.output_item.added' && isRecord(payload.item) && payload.item.type === 'function_call') {
            const call = normalizeToolCall(payload.item, toolCalls.size);
            toolCalls.set(call.id ?? call.name, call);
        }
        if (type === 'response.function_call_arguments.delta' || type === 'response.function_call_arguments.done') {
            const callId = asString(payload.call_id) ?? `call-${toolCalls.size}`;
            const existing = toolCalls.get(callId) ?? { id: callId, name: asString(payload.name) ?? `tool_call_${toolCalls.size + 1}`, arguments: '' };
            existing.name = asString(payload.name) ?? existing.name;
            existing.arguments = type.endsWith('.done')
                ? (asString(payload.arguments) ?? existing.arguments)
                : `${existing.arguments ?? ''}${asString(payload.delta) ?? ''}`;
            existing.raw = payload;
            toolCalls.set(callId, existing);
        }
        if (type === 'response.output_item.done' && isRecord(payload.item) && payload.item.type === 'function_call') {
            const call = normalizeToolCall(payload.item, toolCalls.size);
            toolCalls.set(call.id ?? call.name, call);
        }
    }

    if (completedResponse) {
        const completedItems = parseOpenAIResponsesResponseObject(completedResponse, protocol);
        if (completedItems.length > 0) return { items: completedItems, warnings };
    }

    return {
        items: combineAssistantResponseItems([makeItem({
            id: 'response-responses-stream',
            role: 'assistant',
            source: 'response',
            protocol,
            title: 'assistant response',
            content,
            reasoning,
            toolCalls: [...toolCalls.values()],
            raw: { chunks: events.length },
        })], protocol, { chunks: events.length }),
        warnings,
    };
}

function parseAnthropicResponseObject(payload: JsonObject, protocol: MessageFlowProtocol): MessageFlowItem[] {
    let content = '';
    let reasoning = '';
    const toolCalls: MessageFlowToolCall[] = [];

    for (const block of asArray(payload.content)) {
        if (!isRecord(block)) continue;
        if (block.type === 'text') content += `${content ? '\n' : ''}${asString(block.text) ?? ''}`;
        if (block.type === 'thinking') reasoning += `${reasoning ? '\n' : ''}${asString(block.thinking) ?? ''}`;
        if (block.type === 'tool_use') toolCalls.push(normalizeToolCall(block, toolCalls.length));
    }

    return combineAssistantResponseItems([makeItem({
        id: 'response-anthropic-message',
        role: 'assistant',
        source: 'response',
        protocol,
        title: 'assistant response',
        content,
        reasoning,
        toolCalls,
        raw: payload,
    })], protocol, payload);
}

function mergeAnthropicStream(events: SseEvent[], protocol: MessageFlowProtocol): { items: MessageFlowItem[]; warnings: string[] } {
    const warnings: string[] = [];
    let content = '';
    let reasoning = '';
    const toolCallsByIndex = new Map<number, MessageFlowToolCall>();

    for (const event of events) {
        const payload = parseJson(event.data);
        if (!isRecord(payload)) {
            warnings.push('invalid_sse_chunk');
            continue;
        }
        const type = asString(payload.type) ?? event.event;
        const index = typeof payload.index === 'number' ? payload.index : 0;
        if (type === 'content_block_start' && isRecord(payload.content_block) && payload.content_block.type === 'tool_use') {
            toolCallsByIndex.set(index, normalizeToolCall(payload.content_block, index));
        }
        if (type === 'content_block_delta' && isRecord(payload.delta)) {
            const delta = payload.delta;
            const deltaType = asString(delta.type);
            if (deltaType === 'text_delta') content += asString(delta.text) ?? '';
            if (deltaType === 'thinking_delta') reasoning += asString(delta.thinking) ?? '';
            if (deltaType === 'input_json_delta') {
                const existing = toolCallsByIndex.get(index) ?? { name: `tool_call_${index + 1}`, arguments: '' };
                existing.arguments = `${existing.arguments ?? ''}${asString(delta.partial_json) ?? ''}`;
                existing.raw = delta;
                toolCallsByIndex.set(index, existing);
            }
        }
    }

    return {
        items: combineAssistantResponseItems([makeItem({
            id: 'response-anthropic-stream',
            role: 'assistant',
            source: 'response',
            protocol,
            title: 'assistant response',
            content,
            reasoning,
            toolCalls: [...toolCallsByIndex.values()],
            raw: { chunks: events.length },
        })], protocol, { chunks: events.length }),
        warnings,
    };
}

function parseResponsePayload(content: string | undefined, protocolHint?: string): { protocol: MessageFlowProtocol; items: MessageFlowItem[]; warnings: string[]; error?: string } {
    const warnings: string[] = [];
    const events = parseSse(content);
    const jsonPayload = parseJson(content);
    const hintedProtocol = normalizeProtocolName(protocolHint);
    const protocol = hintedProtocol !== 'unsupported'
        ? hintedProtocol
        : (events.length > 0 ? inferProtocolFromSse(events) : inferProtocolFromResponsePayload(jsonPayload));

    if (events.length > 0) {
        switch (protocol) {
            case 'openai_chat':
                return { protocol, ...mergeChatStream(events, protocol) };
            case 'openai_responses':
                return { protocol, ...mergeResponsesStream(events, protocol) };
            case 'anthropic_messages':
                return { protocol, ...mergeAnthropicStream(events, protocol) };
            default:
                return { protocol, items: [], warnings, error: 'unsupported_protocol' };
        }
    }

    if (!isRecord(jsonPayload)) {
        return { protocol, items: [], warnings, error: content?.trim() ? 'invalid_response_json' : undefined };
    }

    switch (protocol) {
        case 'openai_chat':
            return { protocol, warnings, items: parseOpenAIChatResponseObject(jsonPayload, protocol) };
        case 'openai_responses':
            return { protocol, warnings, items: parseOpenAIResponsesResponseObject(jsonPayload, protocol) };
        case 'anthropic_messages':
            return { protocol, warnings, items: parseAnthropicResponseObject(jsonPayload, protocol) };
        default:
            return { protocol, items: [], warnings, error: 'unsupported_protocol' };
    }
}

export function parseLogMessageFlow(input: ParseLogMessageFlowInput): MessageFlowParseResult {
    const request = parseRequestPayload(input.requestContent, input.requestProtocol);
    const responseContent = input.responseContent?.trim() ? input.responseContent : input.responseFallbackContent;
    const response = parseResponsePayload(responseContent, input.responseProtocol);
    const warnings = [...request.warnings, ...response.warnings];
    if (!input.responseContent?.trim() && input.responseFallbackContent?.trim()) {
        warnings.push('using_stream_preview_fallback');
    }

    const errors = [request.error, response.error].filter(Boolean);
    return {
        sourceMode: input.sourceMode,
        protocolPair: {
            request: request.protocol,
            response: response.protocol,
        },
        items: [...request.items, ...response.items].filter((item) => (
            item.content?.trim() || item.reasoning?.trim() || (item.toolCalls?.length ?? 0) > 0 || (item.tools?.length ?? 0) > 0
        )),
        tools: request.tools,
        warnings,
        error: errors.length > 0 ? errors.join(',') : undefined,
    };
}

export function formatMessageFlowJson(value: unknown): string {
    return stringifyJson(value);
}
