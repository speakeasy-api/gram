[**@gram-ai/elements v1.24.2**](../README.md)

***

[@gram-ai/elements](../globals.md) / useRecordCassette

# Function: useRecordCassette()

> **useRecordCassette**(): `object`

## Returns

### isRecording

> **isRecording**: `boolean`

Whether recording is currently active.

### messageCount

> **messageCount**: `number`

Current number of messages in the thread.

### startRecording()

> **startRecording**: () => `void`

Start recording from the current point in the conversation.

#### Returns

`void`

### stopRecording()

> **stopRecording**: () => `void`

Stop recording.

#### Returns

`void`

### download()

> **download**: (`filename?`) => `void`

Downloads the recorded conversation as a `.cassette.json` file.

#### Parameters

##### filename?

`string`

#### Returns

`void`
