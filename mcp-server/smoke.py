import asyncio
import sys
import json
import os
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

async def main():
    server_params = StdioServerParameters(
        command="wrap.bat",
        args=[],
        env={"PATH": os.environ["PATH"]}
    )

    async with stdio_client(server_params) as (read, write):
        async with ClientSession(read, write) as session:
            try:
                await session.initialize()
                print("Initialized successfully")
                
                print("Calling launch-browser...")
                launch_result = await session.call_tool("launch-browser", {})
                print(f"Launch Result written to launch.json")
                with open("launch.json", "w") as f:
                    f.write(json.dumps(launch_result.model_dump(), indent=2))
                
                print("Calling browser-act (create session)...")
                act_result = await session.call_tool("browser-act", {
                    "session_id": "",
                    "operations": [{"type": "session_create", "url": "https://en.wikipedia.org/wiki/Mangle_(machine_learning)"}]
                })
                print(f"Act Result written to act.json")
                with open("act.json", "w") as f:
                    f.write(json.dumps(act_result.model_dump(), indent=2))
                
                print("Waiting for session to spin up...")
                await asyncio.sleep(2)

                print("Calling browser-observe (sessions)...")
                sessions_result = await session.call_tool("browser-observe", {
                    "session_id": "",
                    "intent": "check_sessions"
                })
                print(f"Sessions Result written to sessions.json")
                with open("sessions.json", "w") as f:
                    f.write(json.dumps(sessions_result.model_dump(), indent=2))
                
                # Extract session ID from sessions list
                import json as json_lib
                try:
                    sess_data = sessions_result.content[0].text
                    sess_json = json_lib.loads(sess_data)
                    sessions_list = sess_json.get("data", {}).get("sessions")
                    if sessions_list and len(sessions_list) > 0:
                        session_id = sessions_list[0].get("id", "")
                    else:
                        print("Warning: No session IDs returned in payload")
                        session_id = ""
                except Exception as e:
                    print(f"Failed to parse session ID (format mismatch): {e}")
                    session_id = ""
                
                print(f"Got Session ID: '{session_id}'")

                print("Calling browser-observe...")
                result = await session.call_tool("browser-observe", {
                    "session_id": session_id,
                    "intent": "quick_status"
                })
                print(f"Observe Result written to observe.json")
                with open("observe.json", "w") as f:
                    f.write(json.dumps(result.model_dump(), indent=2))
            except Exception as e:
                with open("error.log", "w") as f:
                    f.write(f"Error occurred: {e}")
                print(f"Error occurred, see error.log", file=sys.stderr)

if __name__ == "__main__":
    asyncio.run(main())
