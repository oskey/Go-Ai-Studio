export interface LLMProvider {
  id: number;
  name: string;
  provider: string;
  api_address: string;
  api_key: string;
  model_name: string;
  enable_advanced_request_params?: boolean;
  request_max_tokens?: number;
  request_temperature?: number;
  is_active: boolean;
  usage_stats?: LLMProviderUsageStats;
  created_at?: string;
  updated_at?: string;
}

export interface LLMUsageWindowStats {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  request_count: number;
}

export interface LLMUsagePoint {
  label: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  request_count: number;
}

export interface LLMProviderUsageStats {
  total: LLMUsageWindowStats;
  hour: LLMUsageWindowStats;
  day: LLMUsageWindowStats;
  month: LLMUsageWindowStats;
  year: LLMUsageWindowStats;
}

export interface LLMUsageSummary {
  total: LLMUsageWindowStats;
  hour: LLMUsageWindowStats;
  day: LLMUsageWindowStats;
  month: LLMUsageWindowStats;
  year: LLMUsageWindowStats;
  hour_series: LLMUsagePoint[];
  day_series: LLMUsagePoint[];
  month_series: LLMUsagePoint[];
  year_series: LLMUsagePoint[];
  last_flushed?: string;
}

export interface Workflow {
  workflow_name: string;
  type: string;
}

export interface SystemLog {
  id: number;
  level: string;
  message: string;
  details: string;
  created_at: string;
}

export interface SystemLogListResponse {
  items: SystemLog[];
  total: number;
  page: number;
  limit: number;
  total_pages: number;
}

export interface LLMStreamState {
  id: number;
  task_id: string;
  provider_name: string;
  label: string;
  status: string;
  content: string;
  char_count: number;
  created_at: string;
  updated_at: string;
}

export interface ArtStyle {
  id: number;
  name: string;
  description: string;
  created_at?: string;
  updated_at?: string;
}

export interface AutoGenerateTag {
  id: number;
  name: string;
  slug: string;
  description: string;
  rules: string;
  sort_order: number;
  created_at?: string;
  updated_at?: string;
}

export interface GeneralGuideTag {
  id: number;
  name: string;
  slug: string;
  description: string;
  rules: string;
  sort_order: number;
  created_at?: string;
  updated_at?: string;
}

export interface AudioCloneProject {
  id: number;
  name: string;
  code: string;
  description: string;
  script_text: string;
  created_at?: string;
  updated_at?: string;
}

export interface AudioCloneCharacter {
  id: number;
  project_id: number;
  sort_order: number;
  name: string;
  reference_audio: string;
  reference_text: string;
  reference_text_status?: string;
  reference_text_current_task_id?: string;
  reference_text_error?: string;
  created_at?: string;
  updated_at?: string;
}

export interface AudioCloneLine {
  id: number;
  project_id: number;
  sort_order: number;
  character_name: string;
  text: string;
  seed?: number;
  status: string;
  current_task_id?: string;
  last_error?: string;
  generated_audio: string;
  generated_workflow?: string;
  created_at?: string;
  updated_at?: string;
}

export interface QwenTTSProject {
  id: number;
  name: string;
  code: string;
  description: string;
  script_text: string;
  instruct: string;
  temperature: number;
  x_vector_only?: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface QwenTTSCharacter {
  id: number;
  project_id: number;
  sort_order: number;
  name: string;
  reference_audio: string;
  reference_text: string;
  reference_text_status?: string;
  reference_text_current_task_id?: string;
  reference_text_error?: string;
  reference_test_audio?: string;
  created_at?: string;
  updated_at?: string;
}

export interface QwenTTSLine {
  id: number;
  project_id: number;
  sort_order: number;
  character_name: string;
  text: string;
  instruct?: string;
  temperature?: number;
  seed?: number;
  status: string;
  current_task_id?: string;
  last_error?: string;
  generated_audio: string;
  generated_workflow?: string;
  created_at?: string;
  updated_at?: string;
}

export interface AudioProductionProject {
  id: number;
  mode: "custom_voice" | "voice_prompt";
  name: string;
  code: string;
  description: string;
  text: string;
  speaker: string;
  instruct: string;
  voice_instruction: string;
  temperature: number;
  created_at?: string;
  updated_at?: string;
}

export interface AudioProductionLine {
  id: number;
  project_id: number;
  sort_order: number;
  text: string;
  speaker: string;
  instruct: string;
  voice_instruction: string;
  temperature: number;
  status: string;
  current_task_id?: string;
  last_error?: string;
  generated_audio: string;
  generated_workflow?: string;
  created_at?: string;
  updated_at?: string;
}

export interface AudioProductionPresetOption {
  label: string;
  value: string;
  description?: string;
  temperature?: number;
}

export interface MultiVisualProject {
  id: number;
  name: string;
  code: string;
  visual_type?: "character" | "prop" | "scene";
  description: string;
  reference_image: string;
  status: string;
  current_task_id?: string;
  last_error?: string;
  created_at?: string;
  updated_at?: string;
}

export interface MultiVisualImage {
  id: number;
  project_id: number;
  sort_order: number;
  label: string;
  training_tag: string;
  shot_size_label: string;
  view_label: string;
  horizontal_angle: number;
  vertical_angle: number;
  zoom: number;
  camera_view: string;
  status: string;
  generated_image: string;
  created_at?: string;
  updated_at?: string;
}

export interface StoreVisitProject {
  id: number;
  name: string;
  code: string;
  description: string;
  auto_generate_content?: string;
  blogger_reference_image: string;
  selected_blogger_reference_id?: number;
  created_at?: string;
  updated_at?: string;
}

export interface StoreVisitBloggerReference {
  id: number;
  project_id: number;
  sort_order: number;
  image_path: string;
  is_selected: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface StoreVisitSpot {
  id: number;
  project_id: number;
  sort_order: number;
  spot_type?: string;
  name: string;
  intro_text: string;
  reference_image: string;
  image_positive_prompt: string;
  image_negative_prompt: string;
  video_positive_prompt: string;
  video_negative_prompt: string;
  video_duration_seconds: number;
  video_width: number;
  video_height: number;
  image_status: string;
  image_current_task_id?: string;
  image_last_error?: string;
  generated_image: string;
  image_generated_workflow?: string;
  video_status: string;
  video_current_task_id?: string;
  video_last_error?: string;
  generated_video: string;
  video_generated_workflow?: string;
  created_at?: string;
  updated_at?: string;
}

export interface StoreVisitDishGenerationItem {
  id: number;
  project_id: number;
  spot_id: number;
  sort_order: number;
  preset_key: string;
  frames: string[];
  segments: Array<{
    prompt: string;
    duration_seconds: number;
  }>;
  video_status: string;
  video_current_task_id?: string;
  video_last_error?: string;
  generated_video: string;
  video_generated_workflow?: string;
  created_at?: string;
  updated_at?: string;
}

export interface GeneralGuideProject {
  id: number;
  name: string;
  code: string;
  description: string;
  presenter_gender?: "male" | "female";
  presenter_persona?:
    | "female_natural"
    | "female_playful"
    | "female_sexy"
    | "female_gentle"
    | "male_natural"
    | "male_steady"
    | "male_confident"
    | "male_warm";
  auto_generate_content?: string;
  tag_ids?: number[];
  presenter_reference_image: string;
  selected_reference_id?: number;
  current_planning_task_id?: string;
  last_planning_error?: string;
  created_at?: string;
  updated_at?: string;
}

export interface GeneralGuideReference {
  id: number;
  project_id: number;
  sort_order: number;
  image_path: string;
  is_selected: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface GeneralGuideScene {
  id: number;
  project_id: number;
  sort_order: number;
  title: string;
  scene_type: "presenter_scene" | "material_scene" | "closing_scene";
  environment_type: "indoor" | "outdoor";
  need_presenter: boolean;
  image_preset: "presenter_front_halfbody" | "presenter_room_foreground" | "presenter_seated_table" | "material_only";
  upload_headline: string;
  upload_requirement: string;
  intro_text: string;
  reference_image: string;
  image_positive_prompt: string;
  image_negative_prompt: string;
  video_positive_prompt: string;
  video_negative_prompt: string;
  video_duration_seconds: number;
  video_width: number;
  video_height: number;
  image_status: string;
  image_current_task_id?: string;
  image_last_error?: string;
  generated_image: string;
  image_generated_workflow?: string;
  video_status: string;
  video_current_task_id?: string;
  video_last_error?: string;
  generated_video: string;
  video_generated_workflow?: string;
  created_at?: string;
  updated_at?: string;
}

export interface GeneralGuideTransition {
  id: number;
  project_id: number;
  from_scene_id: number;
  to_scene_id: number;
  from_sort_order: number;
  to_sort_order: number;
  transition_prompt: string;
  duration_seconds: number;
  frames_from_end: number;
  tail_frame_image: string;
  tail_frame_source_video?: string;
  video_status: string;
  video_current_task_id?: string;
  video_last_error?: string;
  generated_video: string;
  video_generated_workflow?: string;
  created_at?: string;
  updated_at?: string;
}

export interface GeneralGuideTransitionPresetOption {
  key: string;
  label: string;
  description: string;
  prompt: string;
  recommended_duration_seconds: number;
}

export interface GeneralGuideTransitionPresetListResponse {
  engine: "ltx2_3" | "wan2_2" | "ffmpeg";
  presets: GeneralGuideTransitionPresetOption[];
}

export interface LocalizedPromptText {
  zh: string;
  en: string;
}

export interface Project {
  id: number;
  name: string;
  code: string;
  art_style_id: number;
  art_style?: ArtStyle;
  description: string;
  scene_mode?: number;
  disable_reference_images?: boolean;
  is_empty?: boolean;
  has_auto_generate_draft?: boolean;
  auto_generate_draft_stage?: string;
  next_auto_generate_episode?: number;
  created_at?: string;
  updated_at?: string;
}

export interface AutoGenerateDraft {
  id: number;
  project_id: number;
  current_task_id?: string;
  plot: string;
  episode: number;
  generation_mode?: "narration" | "high_quality" | "storyboard";
  tag_ids?: number[];
  allow_character_speech?: boolean;
  disable_reference_images: boolean;
  mode?: "append" | "overwrite";
  stage: string;
  characters_json: string;
  outline_json?: string;
  scenes_json: string;
  episode_memory_json?: string;
  completed_scenes: number;
  last_error: string;
  created_at?: string;
  updated_at?: string;
}

export interface Character {
  id: number;
  project_id: number;
  name: string;
  gender: string;
  age?: string;
  body_height?: string;
  era?: string;
  country?: string;
  appearance?: string;
  is_locked?: boolean;
  face_fingerprint?: string;
  description: string;
  fingerprint: string;
  positive_prompt: string;
  negative_prompt: string;
  width: number;
  height: number;
  seed: number;
  optimize_clothing: boolean;
  ref_image: string;
  use_ref_image: boolean;
  status: string;
  generated_image: string;
  generated_workflow?: string;
}

export interface Scene {
  id: number;
  project_id: number;
  episode: number;
  scene_id?: number;
  scene_number: number;
  duration_seconds?: number;
  name: string;
  location_id?: string;
  outline_ref?: string;
  scene_goal?: string;
  character_blocking?: string;
  camera_lock?: string;
  description: string;
  image_prompt?: string;
  video_prompt?: string;
  narration: string;
  video_fingerprint?: string;
  positive_prompt: string;
  negative_prompt: string;
  width: number;
  height: number;
  seed: number;
  status: string;
  generated_image: string;
  generated_workflow?: string;
  characters?: Character[]; // Bound characters
  character_ids?: number[]; // For form submission
}

export interface Video {
  id: number;
  project_id: number;
  scene_id: number;
  scene?: Scene;
  narration: string;
  video_fingerprint?: string;
  video_prompt?: string;
  duration_seconds?: number;
  positive_prompt: string;
  negative_prompt: string;
  width: number;
  height: number;
  seed: number;
  status: string;
  jm_task_id?: string;
  generated_video: string;
  generated_workflow?: string;
  segments?: VideoSegment[];
  created_at?: string;
  updated_at?: string;
}

export interface VideoSegment {
  id: number;
  video_id: number;
  segment_index: number;
  start_second: number;
  end_second: number;
  duration_seconds: number;
  fps: number;
  length: number;
  positive_prompt: string;
  negative_prompt: string;
  player_desc: string;
  status: string;
  generated_video: string;
  transition_frame: string;
  created_at?: string;
  updated_at?: string;
}
