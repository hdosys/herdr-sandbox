local status_path = os.getenv("HERDR_SANDBOX_AUDIO_SMOKE_STATUS")
if status_path == nil or status_path == "" then
  return
end

local track = reaper.GetTrack(0, 0)
if track == nil then
  reaper.InsertTrackAtIndex(0, true)
  track = reaper.GetTrack(0, 0)
end

local status = "FX_NOT_FOUND"
if track ~= nil then
  local fx = reaper.TrackFX_AddByName(track, "VST3:AGridder (e47) (32ch)", false, -1)
  if fx >= 0 then
    status = "FX_INSERTED idx=" .. fx
  end
end

local file = io.open(status_path, "w")
if file then
  file:write(status)
  file:close()
end
